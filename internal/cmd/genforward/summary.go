package main

import (
	"log"
	"sort"
)

// printSummary logs, per package, how many exported symbols ended up in
// forward.go vs forward_js.go (or, for host-incompatible packages, in the
// single unconstrained forward.go).
func printSummary(pkgs []*pkgInfo) {
	sorted := make([]*pkgInfo, len(pkgs))
	copy(sorted, pkgs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].importPath < sorted[j].importPath })

	for _, p := range sorted {
		name := p.relDir
		if name == "" {
			name = "."
		}
		switch {
		case p.hostOK:
			log.Printf("%-40s forward.go=%d forward_js.go=%d", name, len(p.common), len(p.jsOnly))
		default:
			log.Printf("%-40s forward.go=%d (host build not supported by source package)", name, len(p.all))
		}
	}
}
