package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// moduleInfo holds the bits of a go.mod file genforward cares about.
type moduleInfo struct {
	path      string // module path, e.g. "github.com/syumai/workers"
	goVersion string // the "go" directive, e.g. "1.22.0"
}

// readModule parses "<dir>/go.mod" and returns its module path and go
// directive. The module path is never hardcoded by genforward: it is always
// read from the source module's own go.mod so this tool keeps working
// unchanged across the github.com/syumai/workers -> workers-go rename.
func readModule(dir string) (*moduleInfo, error) {
	gomodPath := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", gomodPath, err)
	}
	f, err := modfile.Parse(gomodPath, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", gomodPath, err)
	}
	if f.Module == nil {
		return nil, fmt.Errorf("%s has no module directive", gomodPath)
	}
	goVersion := ""
	if f.Go != nil {
		goVersion = f.Go.Version
	}
	return &moduleInfo{
		path:      f.Module.Mod.Path,
		goVersion: goVersion,
	}, nil
}

// writeGoMod writes the mirror's go.mod:
//   - a "Deprecated:" comment immediately above the module directive, so the
//     go command and pkg.go.dev show the mirror module as deprecated.
//   - the same go directive as the source module.
//   - a require for the source module at cfg.version.
//   - a replace directive, if cfg.replace was given (for local testing).
//
// genforward deliberately does not run `go mod tidy`: the required version
// may not exist on the module proxy yet (e.g. it was just tagged), so tidying
// here would fail. It prints a hint for the caller to run it instead.
func writeGoMod(outDir string, cfg *config, srcMod *moduleInfo) error {
	var b []byte
	b = append(b, fmt.Sprintf("// Deprecated: use %s instead.\n", srcMod.path)...)
	b = append(b, fmt.Sprintf("module %s\n\n", cfg.mirrorModule)...)
	if srcMod.goVersion != "" {
		b = append(b, fmt.Sprintf("go %s\n\n", srcMod.goVersion)...)
	}
	b = append(b, fmt.Sprintf("require %s %s\n", srcMod.path, cfg.version)...)
	if cfg.replace != "" {
		replaceDir, err := filepath.Abs(cfg.replace)
		if err != nil {
			return fmt.Errorf("resolving -replace: %w", err)
		}
		b = append(b, fmt.Sprintf("\nreplace %s => %s\n", srcMod.path, replaceDir)...)
	}
	return os.WriteFile(filepath.Join(outDir, "go.mod"), b, 0o644)
}
