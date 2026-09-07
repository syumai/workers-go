package gen

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/syumai/workers-go/scripts/gen-bindings/cfgen/ir"
)

// OverloadEntry selects one overload (by its 0-based position among same-
// named members) of an overloaded method to generate as a distinct Go
// method named Name. Literal, if set, names the string literal value that
// one of the overload's parameters must have (a "discriminant" parameter,
// e.g. `type: "text"`); that parameter is dropped from the generated Go
// signature and passed as a constant at the call site instead. Overloads
// whose index isn't listed in the entries for a given method are skipped
// (with a warning) rather than generated.
type OverloadEntry struct {
	Index   int    `yaml:"index"`
	Name    string `yaml:"name"`
	Literal string `yaml:"literal"`
}

// Overrides is the decoded form of exp/internal/gen/overrides/<pkg>.yaml.
type Overrides struct {
	Package     string                     `yaml:"package"`
	Doc         string                     `yaml:"doc"`
	Include     []string                   `yaml:"include"`
	Bindings    []string                   `yaml:"bindings"`
	Rename      map[string]string          `yaml:"rename"`
	Types       map[string]string          `yaml:"types"`
	Overloads   map[string][]OverloadEntry `yaml:"overloads"`
	Handwritten []string                   `yaml:"handwritten"`
	Exclude     []string                   `yaml:"exclude"`

	// Path is the source file this was loaded from, for error messages.
	Path string `yaml:"-"`
}

// LoadOverrides reads and decodes a single overrides YAML file.
func LoadOverrides(path string) (*Overrides, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var o Overrides
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	o.Path = path
	if o.Package == "" {
		return nil, fmt.Errorf("%s: package is required", path)
	}
	return &o, nil
}

// declRef splits a "Decl", "Decl.member", or "Decl.method.param" override
// key into its dot-separated parts.
func declRef(key string) []string {
	return strings.Split(key, ".")
}

// Validate checks that every declaration and member name referenced by the
// overrides file actually exists in the IR and is part of this package's
// include list. It returns an error describing the first problem found.
func (o *Overrides) Validate(doc *ir.IR) error {
	declByName := indexDecls(doc)

	if len(o.Include) == 0 {
		return fmt.Errorf("%s: include must not be empty", o.Path)
	}
	included := map[string]*ir.Decl{}
	for _, name := range o.Include {
		d, ok := declByName[name]
		if !ok {
			return fmt.Errorf("%s: include: declaration %q not found in IR", o.Path, name)
		}
		included[name] = d
	}

	checkDeclMember := func(key string) error {
		parts := declRef(key)
		declName := parts[0]
		d, ok := included[declName]
		if !ok {
			return fmt.Errorf("%s: %q refers to declaration %q which is not in include", o.Path, key, declName)
		}
		if len(parts) == 1 {
			return nil
		}
		memberOrMethod := parts[1]
		if !declHasMember(declByName, d, memberOrMethod) {
			return fmt.Errorf("%s: %q refers to member %q which does not exist on %q", o.Path, key, memberOrMethod, declName)
		}
		switch len(parts) {
		case 2:
			return nil
		case 3:
			// "Decl.method.returns" (types:) or "Decl.method.param" (rename:).
			param := parts[2]
			if param == "returns" {
				return nil
			}
			if !methodHasParam(d, memberOrMethod, param) {
				return fmt.Errorf("%s: %q refers to param %q which does not exist on %q.%q", o.Path, key, param, declName, memberOrMethod)
			}
			return nil
		case 4:
			// "Decl.method.params.<name>" (types:).
			if parts[2] != "params" {
				return fmt.Errorf("%s: %q is not a recognized override key shape", o.Path, key)
			}
			param := parts[3]
			if !methodHasParam(d, memberOrMethod, param) {
				return fmt.Errorf("%s: %q refers to param %q which does not exist on %q.%q", o.Path, key, param, declName, memberOrMethod)
			}
			return nil
		default:
			return fmt.Errorf("%s: %q is not a recognized override key shape", o.Path, key)
		}
	}

	for _, name := range o.Bindings {
		if _, ok := included[name]; !ok {
			return fmt.Errorf("%s: bindings: declaration %q is not in include", o.Path, name)
		}
	}
	for k := range o.Rename {
		if err := checkDeclMember(k); err != nil {
			return err
		}
	}
	for k := range o.Types {
		if err := checkDeclMember(k); err != nil {
			return err
		}
	}
	for k, entries := range o.Overloads {
		if err := checkDeclMember(k); err != nil {
			return err
		}
		if len(entries) == 0 {
			return fmt.Errorf("%s: overloads: %q must list at least one entry", o.Path, k)
		}
		seen := map[int]bool{}
		for _, e := range entries {
			if e.Name == "" {
				return fmt.Errorf("%s: overloads: %q: entry at index %d must set name", o.Path, k, e.Index)
			}
			if e.Index < 0 {
				return fmt.Errorf("%s: overloads: %q: index must be >= 0, got %d", o.Path, k, e.Index)
			}
			if seen[e.Index] {
				return fmt.Errorf("%s: overloads: %q: index %d listed more than once", o.Path, k, e.Index)
			}
			seen[e.Index] = true
		}
	}
	for _, k := range o.Handwritten {
		if err := checkDeclMember(k); err != nil {
			return err
		}
	}
	for _, k := range o.Exclude {
		if err := checkDeclMember(k); err != nil {
			return err
		}
	}
	return nil
}

func indexDecls(doc *ir.IR) map[string]*ir.Decl {
	m := make(map[string]*ir.Decl, len(doc.Decls))
	for i := range doc.Decls {
		d := &doc.Decls[i]
		m[d.Name] = d
	}
	return m
}

// declHasMember reports whether d has a property or method named name,
// including one contributed by a resolvable extends/intersection
// composition (see resolveDataMembers) or, for a handle-shaped declaration,
// by a resolvable handle-extends ancestor (see resolveHandleMembers) —
// rather than only d's own direct members.
func declHasMember(declByName map[string]*ir.Decl, d *ir.Decl, name string) bool {
	if members, ok := resolveDataMembers(declByName, d); ok {
		for _, m := range members {
			if m.Name == name {
				return true
			}
		}
		return false
	}
	for _, m := range resolveHandleMembers(declByName, d, 0) {
		if m.Name == name {
			return true
		}
	}
	return false
}

func methodHasParam(d *ir.Decl, method, param string) bool {
	for _, m := range d.Members {
		if m.Kind != "method" || m.Name != method {
			continue
		}
		for _, p := range m.Params {
			if p.Name == param {
				return true
			}
		}
	}
	return false
}
