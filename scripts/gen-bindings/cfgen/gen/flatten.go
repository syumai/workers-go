package gen

import (
	"reflect"

	"github.com/syumai/workers-go/scripts/gen-bindings/cfgen/ir"
)

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
		case "union":
			return resolveUnionMembers(declByName, t.Types, depth+1)
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
	case "union":
		return resolveUnionMembers(declByName, t.Types, depth)
	default:
		return nil, false
	}
}

// resolveUnionMembers implements tmp/06-codegen-spec.md 2.1 item 5: when
// every branch of a union resolves to a data shape (an object literal, a
// ref to a data-shaped declaration, or a nested union/intersection of
// those), the branches are merged into one data type:
//
//   - A field present in every branch, with the same type in each, is kept
//     as-is (required if required everywhere, optional if optional in any
//     branch).
//   - A field present in every branch where each branch's type is a
//     boolean literal (true/false) or a plain boolean is unified into a
//     single required bool field (the common R2Objects/KVNamespaceListResult
//     "discriminant" pattern: `{ done: true, ... } | { done: false, ... }`).
//   - A field present in only some branches becomes optional, using the
//     first branch's definition of it.
//   - A field present in every branch but with genuinely conflicting,
//     non-boolean types fails the whole union (ok is false), same as an
//     unresolvable intersection operand.
//
// Any branch that doesn't itself resolve to a data shape (a method/getter
// member, or an operand resolveOperand can't resolve at all) fails the
// whole union.
func resolveUnionMembers(declByName map[string]*ir.Decl, types []ir.Type, depth int) ([]ir.Member, bool) {
	if depth > maxFlattenDepth || len(types) == 0 {
		return nil, false
	}
	branches := make([][]ir.Member, 0, len(types))
	for _, t := range types {
		m, ok := resolveOperand(declByName, t, depth+1)
		if !ok {
			return nil, false
		}
		for _, mm := range m {
			if mm.Kind != "property" {
				return nil, false
			}
		}
		branches = append(branches, m)
	}
	if len(branches) == 1 {
		return branches[0], true
	}

	var order []string
	byName := map[string][]ir.Member{}
	for _, branch := range branches {
		for _, m := range branch {
			if _, ok := byName[m.Name]; !ok {
				order = append(order, m.Name)
			}
			byName[m.Name] = append(byName[m.Name], m)
		}
	}

	var out []ir.Member
	for _, name := range order {
		entries := byName[name]
		presentEverywhere := len(entries) == len(branches)
		switch {
		case presentEverywhere && allBooleanish(entries):
			m := entries[0]
			m.Type = &ir.Type{K: "prim", Name: "boolean"}
			m.Optional = false
			out = append(out, m)
		case presentEverywhere && allSameType(entries):
			m := entries[0]
			m.Optional = anyOptional(entries)
			out = append(out, m)
		case presentEverywhere:
			// Same field, incompatible types across branches: not
			// representable as one merged field.
			return nil, false
		default:
			m := entries[0]
			m.Optional = true
			out = append(out, m)
		}
	}
	return out, true
}

// allBooleanish reports whether every member's type is a boolean literal
// (true or false), a plain boolean, or a union of only those.
func allBooleanish(members []ir.Member) bool {
	for _, m := range members {
		if !isBooleanish(m.Type) {
			return false
		}
	}
	return true
}

func isBooleanish(t *ir.Type) bool {
	if t == nil {
		return false
	}
	switch t.K {
	case "prim":
		return t.Name == "boolean"
	case "literal":
		_, ok := t.Value.(bool)
		return ok
	case "union":
		for i := range t.Types {
			if !isBooleanish(&t.Types[i]) {
				return false
			}
		}
		return len(t.Types) > 0
	default:
		return false
	}
}

func allSameType(members []ir.Member) bool {
	first := members[0].Type
	for _, m := range members[1:] {
		if !reflect.DeepEqual(first, m.Type) {
			return false
		}
	}
	return true
}

func anyOptional(members []ir.Member) bool {
	for _, m := range members {
		if m.Optional {
			return true
		}
	}
	return false
}

// resolveHandleMembers computes the flattened method/getter/property member
// list for a handle-shaped declaration (one with at least one method or
// getter), per tmp/06-codegen-spec.md 2.1 item 4: a handle extending
// another handle (R2ObjectBody extends R2Object) inherits the ancestor's
// members too. Unlike resolveDataMembers, members with the same name are
// not merged field-by-field (a handle's own members may legitimately repeat
// a name for method overloads); instead, an ancestor's member is dropped
// wholesale whenever the child declares any member (of any kind) with the
// same name, and the child's own members (including all of its own
// overloads) are kept unchanged and appended after the (filtered) inherited
// ones.
func resolveHandleMembers(declByName map[string]*ir.Decl, d *ir.Decl, depth int) []ir.Member {
	if depth > maxFlattenDepth || len(d.Extends) == 0 {
		return d.Members
	}
	ownNames := map[string]bool{}
	for _, m := range d.Members {
		ownNames[m.Name] = true
	}
	var inherited []ir.Member
	for _, opTy := range d.Extends {
		if opTy.K != "ref" {
			continue
		}
		anc, ok := declByName[opTy.Name]
		if !ok {
			continue
		}
		for _, m := range resolveHandleMembers(declByName, anc, depth+1) {
			if ownNames[m.Name] {
				continue
			}
			inherited = append(inherited, m)
		}
	}
	return append(inherited, d.Members...)
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
