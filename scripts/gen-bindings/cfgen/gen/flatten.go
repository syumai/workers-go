package gen

import "github.com/syumai/workers-go/scripts/gen-bindings/cfgen/ir"

// maxFlattenDepth guards against pathological or (accidentally) cyclic
// extends/intersection chains; real workers-types declarations never nest
// anywhere near this deep.
const maxFlattenDepth = 20

// resolveDataMembers computes the flattened property member list for d, per
// tmp/06-codegen-spec.md section 1.3 "intersection / extends: data 型同士の
// intersection と extends はフィールドを平坦化してマージする":
//
//   - An alias whose type is an object literal yields that literal's members
//     directly.
//   - An alias whose type is an intersection yields the merged members of
//     every operand, but only if *every* operand resolves to a data shape
//     (a plain interface/class, an object literal, or another such alias).
//     A single unresolvable operand (a TS utility type like Pick/Omit, a
//     ref to something not in the IR, ...) fails the whole alias: ok is
//     false, and the caller should fall back to treating it as an opaque
//     type (see convFor's "intersection" case), since silently dropping
//     most of an intersection's fields would be misleading.
//   - An interface/class yields its own members merged on top of whatever
//     of its `extends` list can be resolved; an unresolvable (or absent)
//     extends operand simply contributes no fields rather than failing the
//     whole declaration, since the declaration's own members are always
//     available on their own. Own members win over inherited ones with the
//     same name, keeping the inherited field's position.
//   - Anything with a method or getter member (handle-shaped) is not a data
//     type at all: ok is false.
func resolveDataMembers(declByName map[string]*ir.Decl, d *ir.Decl) ([]ir.Member, bool) {
	return resolveDataMembersDepth(declByName, d, 0)
}

func resolveDataMembersDepth(declByName map[string]*ir.Decl, d *ir.Decl, depth int) ([]ir.Member, bool) {
	if depth > maxFlattenDepth {
		return nil, false
	}
	if d.Kind == "alias" {
		t := d.Type
		if t == nil {
			return nil, false
		}
		switch t.K {
		case "object":
			return t.Members, true
		case "intersection":
			var merged []ir.Member
			for _, opTy := range t.Types {
				m, ok := resolveOperand(declByName, opTy, depth+1)
				if !ok {
					return nil, false
				}
				merged = mergeMembers(merged, m)
			}
			return merged, true
		default:
			return nil, false
		}
	}

	// interface / class
	for _, m := range d.Members {
		if m.Kind == "method" || m.Kind == "getter" {
			return nil, false
		}
	}
	if len(d.Extends) == 0 {
		return d.Members, true
	}
	var inherited []ir.Member
	for _, opTy := range d.Extends {
		m, ok := resolveOperand(declByName, opTy, depth+1)
		if !ok {
			continue // best-effort: an unresolvable extends operand contributes nothing
		}
		inherited = mergeMembers(inherited, m)
	}
	return mergeMembers(inherited, d.Members), true
}

// resolveOperand resolves one constituent of an extends list or
// intersection into a member list.
func resolveOperand(declByName map[string]*ir.Decl, t ir.Type, depth int) ([]ir.Member, bool) {
	switch t.K {
	case "ref":
		d, ok := declByName[t.Name]
		if !ok {
			return nil, false
		}
		return resolveDataMembersDepth(declByName, d, depth)
	case "object":
		return t.Members, true
	default:
		return nil, false
	}
}

// mergeMembers returns base with overrides merged in: an override whose
// name matches an existing member in base replaces it in place (keeping
// base's ordering), and any new names are appended in order.
func mergeMembers(base, overrides []ir.Member) []ir.Member {
	if len(overrides) == 0 {
		return base
	}
	out := make([]ir.Member, len(base), len(base)+len(overrides))
	copy(out, base)
	index := make(map[string]int, len(out))
	for i, m := range out {
		index[m.Name] = i
	}
	for _, m := range overrides {
		if i, ok := index[m.Name]; ok {
			out[i] = m
			continue
		}
		index[m.Name] = len(out)
		out = append(out, m)
	}
	return out
}
