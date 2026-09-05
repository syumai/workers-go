// Command genforward generates a "mirror" module: a tree of packages that
// re-export every exported symbol of a source module via type aliases and
// var/const forwarding declarations.
//
// See https://github.com/syumai/workers/issues/173 ("Design of the mirror")
// for the design this tool implements.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	src          string
	out          string
	version      string
	mirrorModule string
	replace      string
	static       string
}

func parseFlags(args []string) (*config, error) {
	fs := flag.NewFlagSet("genforward", flag.ContinueOnError)
	cfg := &config{}
	fs.StringVar(&cfg.src, "src", ".", "root of the source module")
	fs.StringVar(&cfg.out, "out", "", "mirror root directory (required)")
	fs.StringVar(&cfg.version, "version", "", "version of the source module the mirror will require (required)")
	fs.StringVar(&cfg.mirrorModule, "mirror-module", "github.com/syumai/workers", "module path written to the mirror's go.mod")
	fs.StringVar(&cfg.replace, "replace", "", "if given, adds `replace <srcModule> => <dir>` to the mirror go.mod")
	fs.StringVar(&cfg.static, "static", "", "directory whose contents are copied verbatim into the mirror root (README.md, command stubs, ...); nothing is copied when empty")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if cfg.out == "" {
		return nil, fmt.Errorf("-out is required")
	}
	if cfg.version == "" {
		return nil, fmt.Errorf("-version is required")
	}
	return cfg, nil
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("genforward: ")
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			os.Exit(2)
		}
		log.Fatal(err)
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

func run(cfg *config) error {
	srcAbs, err := filepath.Abs(cfg.src)
	if err != nil {
		return fmt.Errorf("resolving -src: %w", err)
	}
	outAbs, err := filepath.Abs(cfg.out)
	if err != nil {
		return fmt.Errorf("resolving -out: %w", err)
	}
	staticAbs := cfg.static
	if staticAbs != "" {
		staticAbs, err = filepath.Abs(staticAbs)
		if err != nil {
			return fmt.Errorf("resolving -static: %w", err)
		}
	}

	if err := checkOutDirSafety(outAbs, srcAbs, staticAbs); err != nil {
		return err
	}

	srcMod, err := readModule(srcAbs)
	if err != nil {
		return fmt.Errorf("reading source go.mod: %w", err)
	}
	log.Printf("source module: %s (go %s)", srcMod.path, srcMod.goVersion)

	pkgs, err := collectPackages(srcAbs, srcMod.path)
	if err != nil {
		return fmt.Errorf("collecting packages: %w", err)
	}
	log.Printf("found %d forwardable package(s)", len(pkgs))

	if err := resetOutDir(outAbs); err != nil {
		return fmt.Errorf("resetting -out directory: %w", err)
	}

	for _, p := range pkgs {
		if err := writePackage(outAbs, p); err != nil {
			return fmt.Errorf("writing package %s: %w", p.importPath, err)
		}
	}

	if err := writeGoMod(outAbs, cfg, srcMod); err != nil {
		return fmt.Errorf("writing go.mod: %w", err)
	}

	if err := copyStaticFiles(srcAbs, staticAbs, outAbs); err != nil {
		return fmt.Errorf("copying static files: %w", err)
	}

	printSummary(pkgs)

	fmt.Fprintf(os.Stderr, "\nhint: run the following inside %s to finalize go.sum (requires network access / the version to be published):\n", outAbs)
	fmt.Fprintf(os.Stderr, "  (cd %s && go mod tidy)\n", outAbs)

	return nil
}

// checkOutDirSafety refuses to proceed when outAbs (which resetOutDir is
// about to wipe) has an unsafe relationship to srcAbs or staticAbs: being
// the same directory, an ancestor, or nested inside one of them. Without
// this check, a relative -out given by a caller whose working directory
// happens to coincide with (or sit inside/above) -src or -static can cause
// resetOutDir to delete files it must never touch.
//
// Paths are resolved through any symlinks (where possible; a path that does
// not exist yet, such as -out, is resolved up to its longest existing
// ancestor) before comparison, since on macOS in particular a temp directory
// is commonly reached through a symlink (e.g. /tmp -> /private/tmp), which
// would otherwise defeat a purely lexical comparison.
func checkOutDirSafety(outAbs, srcAbs, staticAbs string) error {
	outR := resolveSymlinks(outAbs)
	srcR := resolveSymlinks(srcAbs)

	switch {
	case outR == srcR:
		return fmt.Errorf("-out (%s) must not be the same directory as -src (%s): it would be wiped before generation, deleting the source module", outAbs, srcAbs)
	case pathContains(outR, srcR):
		return fmt.Errorf("-out (%s) must not be an ancestor of -src (%s): it would be wiped before generation, deleting the source module", outAbs, srcAbs)
	case pathContains(srcR, outR):
		return fmt.Errorf("-out (%s) must not be inside -src (%s): it would be wiped before generation, deleting part of the source module", outAbs, srcAbs)
	}

	if staticAbs != "" {
		staticR := resolveSymlinks(staticAbs)
		switch {
		case outR == staticR:
			return fmt.Errorf("-out (%s) must not be the same directory as -static (%s): it would be wiped before generation, deleting the static files", outAbs, staticAbs)
		case pathContains(outR, staticR):
			return fmt.Errorf("-out (%s) must not contain -static (%s): it would be wiped before generation, deleting the static files", outAbs, staticAbs)
		}
	}

	return nil
}

// pathContains reports whether child is strictly inside parent (not equal to
// it). Both arguments must already be absolute, symlink-resolved paths.
func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveSymlinks resolves symlinks in path, walking up to the longest
// existing ancestor when path itself does not exist yet (as is normal for
// -out, which genforward is about to create). path must be absolute.
func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		// Reached the filesystem root without finding an existing
		// ancestor; nothing more we can resolve.
		return path
	}
	return filepath.Join(resolveSymlinks(parent), filepath.Base(path))
}
