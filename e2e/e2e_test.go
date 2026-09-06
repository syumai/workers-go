//go:build e2e

// Package e2e drives Worker fixtures under wrangler dev (real workerd) to
// check behavior that Node-based fake-JS tests cannot: the actual shape of
// worker.mjs/app.wasm wiring, KV's real API surface, streaming responses,
// and FixedLengthStream. See tmp/test-plan/04-wrangler-e2e.md for the
// design this package implements.
package e2e

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixture describes one testdata/workers/<name> Worker and how TestMain
// (or, for tinygo, the fixture's own test) must build it.
type fixture struct {
	// name is the fixture directory under testdata/workers/.
	name string
	// tinygo, when true, means the fixture is compiled with the tinygo
	// toolchain (workers-assets-gen -mode=tinygo + `tinygo build`)
	// instead of the standard go toolchain. TestMain never builds these
	// fixtures: most contributors don't have tinygo installed, so
	// building is deferred to the fixture's own test, gated by
	// E2E_TINYGO=1 (see buildFixtureTinygo).
	tinygo bool
	// assetsGenArgs are extra arguments appended to the
	// workers-assets-gen invocation, after -mode and -o. Most fixtures
	// leave this nil and take the default (cloudflare) runtime.
	assetsGenArgs []string
}

// fixtures lists the testdata/workers/<name> directories this package
// knows about. TestMain builds every non-tinygo entry (workers-assets-gen
// + GOOS=js GOARCH=wasm) before any test runs.
var fixtures = []fixture{
	{name: "kitchensink"},
	{name: "durableobject"},
	{name: "sockets"},
	{name: "pages"},
	{name: "tinygo", tinygo: true},
}

func TestMain(m *testing.M) {
	// testing.Short() below reads a flag value, so flags must be parsed
	// before we consult it (m.Run() would otherwise parse them for us,
	// but only after this point).
	if !flag.Parsed() {
		flag.Parse()
	}
	// In `go test -short` runs, don't require node/pnpm/wrangler or build
	// anything: leave it to each test to call t.Skip via startWrangler.
	if testing.Short() {
		os.Exit(m.Run())
	}

	for _, bin := range []string{"node", "pnpm"} {
		if _, err := exec.LookPath(bin); err != nil {
			fmt.Fprintf(os.Stderr, "e2e: %q not found in PATH: %v\n", bin, err)
			fmt.Fprintln(os.Stderr, "e2e: install node and pnpm, or run with `go test -short` to skip e2e tests.")
			os.Exit(1)
		}
	}

	repoRoot, err := repoRootDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}
	e2eDir := filepath.Join(repoRoot, "e2e")

	if err := ensureWranglerInstalled(e2eDir); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}

	for _, f := range fixtures {
		if f.tinygo {
			// Built lazily by the tinygo fixture's own test, gated by
			// E2E_TINYGO=1: see buildFixtureTinygo in tinygo_test.go.
			continue
		}
		if err := buildFixture(repoRoot, e2eDir, f); err != nil {
			fmt.Fprintf(os.Stderr, "e2e: failed to build fixture %q: %v\n", f.name, err)
			os.Exit(1)
		}
	}

	os.Exit(m.Run())
}

// repoRootDir returns the root directory of the module e2e/ lives in
// (github.com/syumai/workers-go), derived from `go env GOMOD` so it works
// regardless of the process's current working directory.
func repoRootDir() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %w", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		return "", fmt.Errorf("go env GOMOD returned no module; run e2e tests from within the workers-go module")
	}
	return filepath.Dir(gomod), nil
}

// ensureWranglerInstalled runs `pnpm install --frozen-lockfile` in e2eDir
// unless node_modules/.bin/wrangler already exists.
func ensureWranglerInstalled(e2eDir string) error {
	wranglerBin := filepath.Join(e2eDir, "node_modules", ".bin", "wrangler")
	if _, err := os.Stat(wranglerBin); err == nil {
		return nil
	}
	cmd := exec.Command("pnpm", "install", "--frozen-lockfile")
	cmd.Dir = e2eDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pnpm install --frozen-lockfile (in %s): %w", e2eDir, err)
	}
	return nil
}

// buildFixture generates the wrangler assets (workers-assets-gen) and
// compiles the fixture's app.wasm into
// e2e/testdata/workers/<name>/build/.
func buildFixture(repoRoot, e2eDir string, f fixture) error {
	fixtureDir := filepath.Join(e2eDir, "testdata", "workers", f.name)
	buildDir := filepath.Join(fixtureDir, "build")
	assetsGenDir := filepath.Join(repoRoot, "cmd", "workers-assets-gen")

	genArgs := append([]string{"run", assetsGenDir, "-mode=go", "-o", buildDir}, f.assetsGenArgs...)
	genCmd := exec.Command("go", genArgs...)
	genCmd.Dir = e2eDir
	if out, err := genCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("workers-assets-gen for %q failed: %w\n%s", f.name, err, out)
	}

	buildCmd := exec.Command("go", "build", "-o", filepath.Join(buildDir, "app.wasm"), fixtureDir)
	buildCmd.Dir = e2eDir
	buildCmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("GOOS=js GOARCH=wasm go build for %q failed: %w\n%s", f.name, err, out)
	}
	return nil
}

// buildFixtureTinygo generates the wrangler assets in tinygo mode and
// compiles the fixture's app.wasm with the tinygo toolchain instead of the
// standard go toolchain. Unlike buildFixture, this is never called from
// TestMain: it's invoked lazily by the tinygo fixture's own test, gated by
// E2E_TINYGO=1, since most contributors won't have tinygo installed.
func buildFixtureTinygo(repoRoot, e2eDir, name string) error {
	fixtureDir := filepath.Join(e2eDir, "testdata", "workers", name)
	buildDir := filepath.Join(fixtureDir, "build")
	assetsGenDir := filepath.Join(repoRoot, "cmd", "workers-assets-gen")

	genCmd := exec.Command("go", "run", assetsGenDir, "-mode=tinygo", "-o", buildDir)
	genCmd.Dir = e2eDir
	if out, err := genCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("workers-assets-gen -mode=tinygo for %q failed: %w\n%s", name, err, out)
	}

	buildCmd := exec.Command("tinygo", "build", "-o", filepath.Join(buildDir, "app.wasm"), "-target", "wasm", "-no-debug", "./")
	buildCmd.Dir = fixtureDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tinygo build for %q failed: %w\n%s", name, err, out)
	}
	return nil
}
