package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
)

// srcAlias is the stable local name genforward uses for the aliased import of
// the corresponding source package in every generated file.
const srcAlias = "src"

// writePackage generates the forwarding file(s) for one package and writes
// them under outDir.
func writePackage(outDir string, p *pkgInfo) error {
	newImportPath := p.importPath // the source module already *is* the new import path
	pkgDir := outDir
	if p.relDir != "" {
		pkgDir = filepath.Join(outDir, filepath.FromSlash(p.relDir))
	}
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return err
	}

	if p.hostOK {
		if err := writeForwardFile(pkgDir, "forward.go", p, newImportPath, p.common, "", true); err != nil {
			return err
		}
		if len(p.jsOnly) > 0 {
			if err := writeForwardFile(pkgDir, "forward_js.go", p, newImportPath, p.jsOnly, "//go:build js && wasm\n", false); err != nil {
				return err
			}
		}
		return nil
	}

	// The source package does not build on host at all (e.g. it imports
	// syscall/js unconditionally, as every non-root package in this codebase
	// does). Its forwarder is written, unconstrained, into forward.go too:
	// it will fail to build on host exactly like the source package does,
	// which is expected and not an error condition for genforward.
	return writeForwardFile(pkgDir, "forward.go", p, newImportPath, p.all, "", true)
}

func writeForwardFile(pkgDir, filename string, p *pkgInfo, newImportPath string, syms []symbol, buildTag string, includeDoc bool) error {
	var b bytes.Buffer

	if buildTag != "" {
		b.WriteString(buildTag)
		b.WriteString("\n")
	}

	if includeDoc {
		writeDocComment(&b, p.doc, newImportPath)
	}

	fmt.Fprintf(&b, "package %s\n\n", p.name)

	if len(syms) > 0 {
		fmt.Fprintf(&b, "import (\n\t%s %q\n)\n\n", srcAlias, p.importPath)
	}

	for _, s := range syms {
		switch s.kind {
		case kindType:
			fmt.Fprintf(&b, "type %s = %s.%s\n", s.name, srcAlias, s.name)
		case kindFunc:
			fmt.Fprintf(&b, "var %s = %s.%s\n", s.name, srcAlias, s.name)
		case kindConst:
			fmt.Fprintf(&b, "const %s = %s.%s\n", s.name, srcAlias, s.name)
		case kindVar:
			fmt.Fprintf(&b, "var %s = %s.%s\n", s.name, srcAlias, s.name)
		}
	}

	out, err := format.Source(b.Bytes())
	if err != nil {
		return fmt.Errorf("formatting %s: %w\n---\n%s", filename, err, b.String())
	}
	return os.WriteFile(filepath.Join(pkgDir, filename), out, 0o644)
}

// writeDocComment writes the package doc comment: the original doc (if any)
// followed by a "Deprecated:" paragraph pointing at newImportPath, so
// pkg.go.dev shows the deprecation notice for this package.
func writeDocComment(b *bytes.Buffer, originalDoc, newImportPath string) {
	if originalDoc != "" {
		for _, line := range strings.Split(strings.TrimRight(originalDoc, "\n"), "\n") {
			if line == "" {
				b.WriteString("//\n")
			} else {
				b.WriteString("// ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
		b.WriteString("//\n")
	}
	fmt.Fprintf(b, "// Deprecated: use %s instead.\n", newImportPath)
}
