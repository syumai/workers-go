package main

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/types"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// symbolKind categorizes an exported package-level identifier.
type symbolKind int

const (
	kindType symbolKind = iota
	kindFunc
	kindConst
	kindVar
)

type symbol struct {
	name string
	kind symbolKind
}

// pkgInfo describes one source package and the forwarding files genforward
// will emit for it.
type pkgInfo struct {
	importPath string // full source import path, e.g. "github.com/syumai/workers/cloudflare/kv"
	relDir     string // path relative to the module root ("" for the module root package)
	name       string // Go package name
	doc        string // package doc comment (without the Deprecated notice), may be empty

	// hostOK reports whether the source package builds cleanly on the host
	// platform (the platform genforward itself runs on). When true, symbols
	// are split between common (both platforms) and jsOnly (js/wasm only).
	// When false, the source package does not build on host at all (as is
	// the case for every non-root package in this codebase, since they
	// import syscall/js unconditionally); in that case all of its symbols
	// are written to an unconstrained forward.go, which will simply fail to
	// build on host exactly like the source package does. This is expected
	// and is not treated as an error by genforward.
	hostOK bool

	common []symbol // present on both platforms (only meaningful when hostOK)
	jsOnly []symbol // present only under js/wasm (only meaningful when hostOK)
	all    []symbol // full symbol set (only meaningful when !hostOK)
}

const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
	packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule

// loadPackages loads every package in the module rooted at dir, using env as
// the environment for the underlying `go list` invocation (nil means "use
// genforward's own host platform").
func loadPackages(dir string, env []string) (map[string]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: loadMode,
		Dir:  dir,
		Env:  env,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	m := make(map[string]*packages.Package, len(pkgs))
	for _, p := range pkgs {
		m[p.PkgPath] = p
	}
	return m, nil
}

// collectPackages loads the source module for both the host platform and
// js/wasm, and returns the forwardable packages with their exported symbols
// classified.
func collectPackages(srcDir, srcModule string) ([]*pkgInfo, error) {
	hostPkgs, err := loadPackages(srcDir, nil)
	if err != nil {
		return nil, fmt.Errorf("loading host packages: %w", err)
	}
	wasmEnv := append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	wasmPkgs, err := loadPackages(srcDir, wasmEnv)
	if err != nil {
		return nil, fmt.Errorf("loading js/wasm packages: %w", err)
	}

	// Union of import paths seen on either platform, so we don't miss a
	// package that only exists under one of them.
	seen := make(map[string]bool)
	var order []string
	for _, p := range wasmPkgs {
		if !seen[p.PkgPath] {
			seen[p.PkgPath] = true
			order = append(order, p.PkgPath)
		}
	}
	for _, p := range hostPkgs {
		if !seen[p.PkgPath] {
			seen[p.PkgPath] = true
			order = append(order, p.PkgPath)
		}
	}
	sort.Strings(order)

	var out []*pkgInfo
	for _, importPath := range order {
		if !strings.HasPrefix(importPath, srcModule) {
			// Not part of the source module (shouldn't happen with "./...").
			continue
		}
		relDir := strings.TrimPrefix(strings.TrimPrefix(importPath, srcModule), "/")
		if isInternalRelDir(relDir) {
			continue
		}

		wasmPkg := wasmPkgs[importPath]
		hostPkg := hostPkgs[importPath]

		// Determine the package name and whether it's a main package from
		// whichever load succeeded.
		name := ""
		if wasmPkg != nil {
			name = wasmPkg.Name
		} else if hostPkg != nil {
			name = hostPkg.Name
		}
		if name == "main" {
			continue
		}
		if wasmPkg == nil {
			// A package that only exists on host is not expected in this
			// codebase; fail loudly rather than silently dropping it.
			return nil, fmt.Errorf("package %s exists on host but not under GOOS=js GOARCH=wasm (unexpected for this codebase)", importPath)
		}
		if len(wasmPkg.Errors) > 0 {
			return nil, fmt.Errorf("package %s failed to build under GOOS=js GOARCH=wasm: %v", importPath, wasmPkg.Errors)
		}
		if wasmPkg.Types == nil {
			return nil, fmt.Errorf("package %s has no type information under GOOS=js GOARCH=wasm", importPath)
		}
		if len(wasmPkg.CompiledGoFiles) == 0 {
			// Test-only or otherwise empty package; nothing to forward.
			continue
		}

		hostOK := hostPkg != nil && len(hostPkg.Errors) == 0 && hostPkg.Types != nil

		info := &pkgInfo{
			importPath: importPath,
			relDir:     relDir,
			name:       name,
		}

		wasmNames := exportedNames(wasmPkg.Types.Scope())

		if hostOK {
			hostNames := exportedNames(hostPkg.Types.Scope())
			hostSet := toSet(hostNames)
			wasmSet := toSet(wasmNames)

			for _, n := range hostNames {
				if !wasmSet[n] {
					return nil, fmt.Errorf("package %s: symbol %q exists on host but not under GOOS=js GOARCH=wasm (unexpected for this codebase)", importPath, n)
				}
			}

			for _, n := range wasmNames {
				sym, err := symbolFor(wasmPkg, n)
				if err != nil {
					return nil, fmt.Errorf("package %s: %w", importPath, err)
				}
				if hostSet[n] {
					info.common = append(info.common, sym)
				} else {
					info.jsOnly = append(info.jsOnly, sym)
				}
			}
			info.hostOK = true
			info.doc = packageDoc(hostPkg)
			if info.doc == "" {
				info.doc = packageDoc(wasmPkg)
			}
		} else {
			for _, n := range wasmNames {
				sym, err := symbolFor(wasmPkg, n)
				if err != nil {
					return nil, fmt.Errorf("package %s: %w", importPath, err)
				}
				info.all = append(info.all, sym)
			}
			info.hostOK = false
			info.doc = packageDoc(wasmPkg)
		}

		sortSymbols(info.common)
		sortSymbols(info.jsOnly)
		sortSymbols(info.all)

		out = append(out, info)
	}

	return out, nil
}

func sortSymbols(syms []symbol) {
	sort.Slice(syms, func(i, j int) bool { return syms[i].name < syms[j].name })
}

func toSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// exportedNames returns the exported identifiers declared directly in scope,
// skipping any name the type checker filled in for a broken import (empty
// "" package objects can't occur here since these are always the requested
// package's own scope).
func exportedNames(scope *types.Scope) []string {
	var names []string
	for _, n := range scope.Names() {
		if ast.IsExported(n) {
			names = append(names, n)
		}
	}
	return names
}

// symbolFor classifies the exported object named name in pkg, and enforces
// the constraints genforward requires to emit a valid forwarder:
//   - functions must not be generic (the codebase is expected to have none).
//   - types must not be generic (plain `type X = src.X` aliases can't carry
//     type parameters).
func symbolFor(pkg *packages.Package, name string) (symbol, error) {
	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		return symbol{}, fmt.Errorf("symbol %q not found in package scope", name)
	}

	var kind symbolKind
	switch o := obj.(type) {
	case *types.TypeName:
		kind = kindType
		if named, ok := o.Type().(*types.Named); ok && named.TypeParams().Len() > 0 {
			return symbol{}, fmt.Errorf("type %s has type parameters; genforward cannot express a generic alias here", name)
		}
	case *types.Func:
		kind = kindFunc
		if sig, ok := o.Type().(*types.Signature); ok && sig.TypeParams().Len() > 0 {
			return symbol{}, fmt.Errorf("func %s has type parameters; genforward does not support forwarding generic functions", name)
		}
	case *types.Const:
		kind = kindConst
	case *types.Var:
		kind = kindVar
	default:
		return symbol{}, fmt.Errorf("symbol %s has unsupported object kind %T", name, obj)
	}

	return symbol{name: name, kind: kind}, nil
}

// isInternalRelDir reports whether relDir (a package directory path relative
// to the module root) contains an "internal" path segment.
func isInternalRelDir(relDir string) bool {
	if relDir == "" {
		return false
	}
	for _, seg := range strings.Split(relDir, "/") {
		if seg == "internal" {
			return true
		}
	}
	return false
}

// packageDoc extracts the package-level doc comment from pkg's syntax trees,
// or "" if there is none.
func packageDoc(pkg *packages.Package) string {
	if pkg == nil || len(pkg.Syntax) == 0 {
		return ""
	}
	d, err := doc.NewFromFiles(pkg.Fset, pkg.Syntax, pkg.PkgPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(d.Doc)
}
