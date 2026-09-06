package main

import (
	"bytes"
	"flag"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// update regenerates the golden files under testdata/golden when set.
// Run: go test ./cmd/workers-assets-gen/... -run TestRunMain_fileList -update
var update = flag.Bool("update", false, "update golden files")

func TestRunMain(t *testing.T) {
	tests := map[string]struct {
		mode          Mode
		runtime       Runtime
		wantWasmExecF string
	}{
		"go-cloudflare": {
			mode: ModeGo, runtime: RuntimeCloudflare,
			wantWasmExecF: "wasm_exec_go.js",
		},
		"go-browser": {
			mode: ModeGo, runtime: RuntimeBrowser,
			wantWasmExecF: "wasm_exec_go.js",
		},
		"tinygo-cloudflare": {
			mode: ModeTinygo, runtime: RuntimeCloudflare,
			wantWasmExecF: "wasm_exec_tinygo.js",
		},
		"tinygo-browser": {
			mode: ModeTinygo, runtime: RuntimeBrowser,
			wantWasmExecF: "wasm_exec_tinygo.js",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := runMain(tt.mode, tt.runtime, dir); err != nil {
				t.Fatalf("runMain() error = %v", err)
			}

			wantWasmExec, err := assets.ReadFile(path.Join(assetDirPath, tt.wantWasmExecF))
			if err != nil {
				t.Fatalf("assets.ReadFile() error = %v", err)
			}
			assertFileEqualsBytes(t, filepath.Join(dir, "wasm_exec.js"), wantWasmExec)

			wantRuntime, err := assets.ReadFile(path.Join(runtimeDirPath, tt.runtime.AssetFileName()))
			if err != nil {
				t.Fatalf("assets.ReadFile() error = %v", err)
			}
			assertFileEqualsBytes(t, filepath.Join(dir, "runtime.mjs"), wantRuntime)

			wantWorker, err := assets.ReadFile(path.Join(commonDirPath, "worker.mjs"))
			if err != nil {
				t.Fatalf("assets.ReadFile() error = %v", err)
			}
			assertFileEqualsBytes(t, filepath.Join(dir, "worker.mjs"), wantWorker)
		})
	}
}

// TestRunMain_cleansOutputDir pins the current (dangerous) behavior of
// runMain: it removes the entire output directory (os.RemoveAll) before
// writing to it, so any pre-existing files there -- including files
// unrelated to workers-assets-gen -- are deleted. This is documented here
// so that a future change to this behavior is a deliberate decision, not
// an accident.
func TestRunMain_cleansOutputDir(t *testing.T) {
	dir := t.TempDir()
	staleFile := filepath.Join(dir, "stale.txt")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	if err := runMain(ModeGo, RuntimeCloudflare, dir); err != nil {
		t.Fatalf("runMain() error = %v", err)
	}

	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Errorf("stale file %q survived runMain(); os.Stat error = %v, want os.IsNotExist", staleFile, err)
	}
}

func TestRunMain_invalidMode(t *testing.T) {
	dir := t.TempDir()
	if err := runMain(Mode("invalid"), RuntimeCloudflare, dir); err == nil {
		t.Error("runMain() error = nil, want non-nil for an invalid mode")
	}
}

func TestRunMain_invalidRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := runMain(ModeGo, Runtime("invalid"), dir); err == nil {
		t.Error("runMain() error = nil, want non-nil for an invalid runtime")
	}
}

func TestRunMain_fileList(t *testing.T) {
	tests := map[string]struct {
		mode    Mode
		runtime Runtime
	}{
		"go-cloudflare":     {mode: ModeGo, runtime: RuntimeCloudflare},
		"go-browser":        {mode: ModeGo, runtime: RuntimeBrowser},
		"tinygo-cloudflare": {mode: ModeTinygo, runtime: RuntimeCloudflare},
		"tinygo-browser":    {mode: ModeTinygo, runtime: RuntimeBrowser},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := runMain(tt.mode, tt.runtime, dir); err != nil {
				t.Fatalf("runMain() error = %v", err)
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("os.ReadDir() error = %v", err)
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			sort.Strings(names)
			got := strings.Join(names, "\n") + "\n"

			goldenPath := filepath.Join("testdata", "golden", name+".txt")
			if *update {
				if err := os.MkdirAll(filepath.Dir(goldenPath), os.ModePerm); err != nil {
					t.Fatalf("os.MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("os.ReadFile(%q) error = %v (run with -update to (re)generate)", goldenPath, err)
			}
			if got != string(want) {
				t.Errorf("file list for %s mismatches golden %q\ngot:\n%s\nwant:\n%s", name, goldenPath, got, want)
			}
		})
	}
}

func assertFileEqualsBytes(t *testing.T, filePath string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", filePath, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("file %q content does not match expected asset content", filePath)
	}
}
