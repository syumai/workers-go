// Command cfgen generates Go bindings under exp/cloudflare/<pkg> from the
// JSON IR at exp/internal/gen/ir/index.json and the overrides YAML files
// under exp/internal/gen/overrides/. See tmp/06-codegen-spec.md section 1.3.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/syumai/workers-go/scripts/gen-bindings/cfgen/gen"
	"github.com/syumai/workers-go/scripts/gen-bindings/cfgen/ir"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cfgen:", err)
		os.Exit(1)
	}
}

func run() error {
	root := flag.String("root", "", "path to the repository root")
	pkg := flag.String("pkg", "", "generate only this package (matches the overrides file's package: field)")
	check := flag.Bool("check", false, "check that generated output matches the existing files; exit non-zero on diff")
	flag.Parse()

	if *root == "" {
		return fmt.Errorf("-root is required")
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return err
	}

	irPath := filepath.Join(absRoot, "exp", "internal", "gen", "ir", "index.json")
	doc, err := loadIR(irPath)
	if err != nil {
		return fmt.Errorf("loading IR: %w", err)
	}

	overridesDir := filepath.Join(absRoot, "exp", "internal", "gen", "overrides")
	overridesFiles, err := filepath.Glob(filepath.Join(overridesDir, "*.yaml"))
	if err != nil {
		return err
	}
	sort.Strings(overridesFiles)
	if len(overridesFiles) == 0 {
		return fmt.Errorf("no overrides files found in %s", overridesDir)
	}

	var mismatched []string
	for _, path := range overridesFiles {
		ov, err := gen.LoadOverrides(path)
		if err != nil {
			return err
		}
		if *pkg != "" && ov.Package != *pkg {
			continue
		}
		if err := ov.Validate(doc); err != nil {
			return err
		}
		result, err := gen.Generate(doc, ov)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, w := range result.Warnings {
			fmt.Fprintf(os.Stderr, "cfgen: warning: %s: %s\n", ov.Package, w)
		}

		outDir := filepath.Join(absRoot, "exp", "cloudflare", ov.Package)
		outPath := filepath.Join(outDir, "z"+ov.Package+"_gen.go")

		if *check {
			existing, err := os.ReadFile(outPath)
			if err != nil || !bytes.Equal(existing, result.Source) {
				mismatched = append(mismatched, outPath)
			}
			continue
		}

		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, result.Source, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "cfgen: wrote %s\n", outPath)
	}

	if *check && len(mismatched) > 0 {
		fmt.Fprintln(os.Stderr, "cfgen: generated output is out of date for:")
		for _, m := range mismatched {
			fmt.Fprintf(os.Stderr, "  %s\n", m)
		}
		return fmt.Errorf("run `make gen-bindings` to regenerate")
	}

	return nil
}

func loadIR(path string) (*ir.IR, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc ir.IR
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}
