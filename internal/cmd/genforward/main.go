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
)

type config struct {
	src          string
	out          string
	version      string
	mirrorModule string
	replace      string
}

func parseFlags(args []string) (*config, error) {
	fs := flag.NewFlagSet("genforward", flag.ContinueOnError)
	cfg := &config{}
	fs.StringVar(&cfg.src, "src", ".", "root of the source module")
	fs.StringVar(&cfg.out, "out", "", "mirror root directory (required)")
	fs.StringVar(&cfg.version, "version", "", "version of the source module the mirror will require (required)")
	fs.StringVar(&cfg.mirrorModule, "mirror-module", "github.com/syumai/workers", "module path written to the mirror's go.mod")
	fs.StringVar(&cfg.replace, "replace", "", "if given, adds `replace <srcModule> => <dir>` to the mirror go.mod")
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

	if err := copyStaticFiles(srcAbs, outAbs); err != nil {
		return fmt.Errorf("copying static files: %w", err)
	}

	printSummary(pkgs)

	fmt.Fprintf(os.Stderr, "\nhint: run the following inside %s to finalize go.sum (requires network access / the version to be published):\n", outAbs)
	fmt.Fprintf(os.Stderr, "  (cd %s && go mod tidy)\n", outAbs)

	return nil
}
