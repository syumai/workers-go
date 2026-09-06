//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// wranglerReadyTimeout bounds how long startWrangler waits for `wrangler
// dev` to report it is ready to serve requests.
const wranglerReadyTimeout = 60 * time.Second

// requestTimeout bounds every HTTP request the worker helper makes.
const requestTimeout = 10 * time.Second

// worker is a running `wrangler dev` instance for one fixture.
type worker struct {
	// BaseURL is the fixture's local HTTP origin, e.g. "http://127.0.0.1:12345".
	BaseURL string
	// Client is an http.Client with a per-request timeout. Its Transport
	// disables transparent compression: Go's default Transport silently
	// drops Content-Length and switches to chunked transfer-encoding when
	// it negotiates gzip on the caller's behalf, which would hide the
	// exact behavior tests like fixed/content_length_preserved need to
	// observe.
	Client *http.Client
}

// freePort asks the OS for a currently-unused TCP port on 127.0.0.1.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startWrangler starts `wrangler dev` for the fixture under
// testdata/workers/<fixture>/wrangler.jsonc, waits for it to become ready,
// and registers a t.Cleanup that stops it (SIGTERM, escalating to SIGKILL
// after 5s). The fixture's build/ directory must already exist (TestMain
// builds every fixture before any test runs).
func startWrangler(t *testing.T, fixture string) *worker {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping wrangler e2e test in short mode")
	}

	e2eDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	configPath := filepath.Join(e2eDir, "testdata", "workers", fixture, "wrangler.jsonc")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("fixture config not found: %s: %v", configPath, err)
	}

	httpPort := freePort(t)
	inspectorPort := freePort(t)
	persistDir := t.TempDir()

	args := []string{
		"exec", "wrangler", "dev",
		"--config", configPath,
		"--port", strconv.Itoa(httpPort),
		"--inspector-port", strconv.Itoa(inspectorPort),
		"--persist-to", persistDir,
		"--test-scheduled",
		"--show-interactive-dev-session=false",
		"--log-level", "info",
	}

	return runWrangler(t, "pnpm", e2eDir, args, httpPort, "/healthz")
}

// startWranglerPages starts `wrangler pages dev` for the fixture under
// testdata/workers/<fixture>, serving the fixture's pages/ directory as
// static assets and picking up its functions/ directory (which must live
// alongside pages/, since `wrangler pages dev` discovers Pages Functions
// relative to the working directory rather than via --config). Otherwise
// it behaves like startWrangler: it waits for readiness and registers a
// t.Cleanup that stops the process.
func startWranglerPages(t *testing.T, fixture string) *worker {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping wrangler e2e test in short mode")
	}

	e2eDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	fixtureDir := filepath.Join(e2eDir, "testdata", "workers", fixture)
	pagesDir := filepath.Join(fixtureDir, "pages")
	if _, err := os.Stat(pagesDir); err != nil {
		t.Fatalf("fixture pages dir not found: %s: %v", pagesDir, err)
	}
	// `wrangler pages dev` is invoked directly (not via `pnpm exec`):
	// pnpm's `exec` resets the child process's working directory away
	// from the caller's cwd, which breaks wrangler's cwd-relative
	// discovery of the sibling ./functions directory (there is no
	// --config-style flag to anchor that lookup explicitly, unlike
	// `wrangler dev`). Invoking the installed binary directly with
	// cmd.Dir set to fixtureDir keeps that discovery working.
	wranglerBin := filepath.Join(e2eDir, "node_modules", ".bin", "wrangler")
	if _, err := os.Stat(wranglerBin); err != nil {
		t.Fatalf("wrangler binary not found: %s: %v", wranglerBin, err)
	}

	httpPort := freePort(t)
	inspectorPort := freePort(t)
	persistDir := t.TempDir()

	args := []string{
		"pages", "dev", pagesDir,
		"--port", strconv.Itoa(httpPort),
		"--inspector-port", strconv.Itoa(inspectorPort),
		"--persist-to", persistDir,
		"--compatibility-date", "2025-06-01",
		"--show-interactive-dev-session=false",
		"--log-level", "info",
	}

	return runWrangler(t, wranglerBin, fixtureDir, args, httpPort, "/api/healthz")
}

// runWrangler starts `cmdName ...args` in dir, waits for it to report
// readiness (either a "Ready on http://" log line or a 200 from
// healthzPath), and registers a t.Cleanup that stops it (SIGTERM,
// escalating to SIGKILL after 5s).
func runWrangler(t *testing.T, cmdName, dir string, args []string, httpPort int, healthzPath string) *worker {
	t.Helper()

	cmd := exec.Command(cmdName, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CI=true", "WRANGLER_SEND_METRICS=false")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to attach stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("failed to attach stderr pipe: %v", err)
	}

	t.Logf("starting: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start wrangler dev: %v", err)
	}

	readyCh := make(chan struct{})
	var readyOnce sync.Once
	signalReady := func() { readyOnce.Do(func() { close(readyCh) }) }

	var wg sync.WaitGroup
	logLines := func(prefix string, r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			t.Logf("[%s] %s", prefix, line)
			if strings.Contains(line, "Ready on http://") {
				signalReady()
			}
		}
	}
	wg.Add(2)
	go logLines("wrangler:stdout", stdout)
	go logLines("wrangler:stderr", stderr)

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			<-waitDone
		}
		wg.Wait()
	})

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	client := &http.Client{
		Timeout:   requestTimeout,
		Transport: &http.Transport{DisableCompression: true},
	}

	// Also poll healthzPath: some fixtures may print their "Ready on
	// http://" line before the handler is actually able to serve
	// requests, and relying on either signal alone has been flaky in
	// other wrangler-based test setups.
	pollCh := make(chan struct{})
	pollCtx, cancelPoll := context.WithCancel(context.Background())
	defer cancelPoll()
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-pollCtx.Done():
				return
			case <-ticker.C:
				req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, baseURL+healthzPath, nil)
				if err != nil {
					continue
				}
				resp, err := client.Do(req)
				if err != nil {
					continue
				}
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					signalReady()
					close(pollCh)
					return
				}
			}
		}
	}()

	select {
	case <-readyCh:
	case <-waitDone:
		t.Fatalf("wrangler exited before becoming ready: %s", cmd.String())
	case <-time.After(wranglerReadyTimeout):
		t.Fatalf("timed out after %s waiting for wrangler to become ready: %s", wranglerReadyTimeout, cmd.String())
	}

	return &worker{
		BaseURL: baseURL,
		Client:  client,
	}
}

// Get performs a GET request against path (which may include a query
// string) and returns the response together with its fully-read body.
func (w *worker) Get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	return w.Do(t, http.MethodGet, path, nil, nil)
}

// Do performs an HTTP request against path with the given method, headers,
// and body, and returns the response together with its fully-read body.
func (w *worker) Do(t *testing.T, method, path string, headers http.Header, body io.Reader) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, w.BaseURL+path, body)
	if err != nil {
		t.Fatalf("failed to build request %s %s: %v", method, path, err)
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := w.Client.Do(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body for %s %s: %v", method, path, err)
	}
	return resp, string(b)
}

// Scheduled triggers wrangler's local cron simulation
// (`--test-scheduled`) for the given cron expression.
func (w *worker) Scheduled(t *testing.T, cron string) (*http.Response, string) {
	t.Helper()
	return w.Get(t, "/__scheduled?cron="+url.QueryEscape(cron))
}
