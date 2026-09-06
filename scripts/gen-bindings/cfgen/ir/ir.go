// Package ir defines the Go representation of the JSON intermediate
// representation produced by scripts/gen-bindings/src/extract.ts. The
// schema is documented in tmp/06-codegen-spec.md section 1.2; this file
// mirrors it field for field.
package ir

// Source describes the workers-types package the IR was extracted from.
type Source struct {
	Package string `json:"package"`
	Version string `json:"version"`
	Entry   string `json:"entry"`
}

// IR is the top-level document written by extract.ts.
type IR struct {
	Source Source `json:"source"`
	Decls  []Decl `json:"decls"`
}

// TypeParam is a generic type parameter of a declaration or method.
type TypeParam struct {
	Name    string `json:"name"`
	Default *Type  `json:"default,omitempty"`
}

// Decl is a single top-level declaration: an interface, class, or type
// alias extracted from the .d.ts source.
type Decl struct {
	Kind       string      `json:"kind"` // "interface" | "class" | "alias"
	Name       string      `json:"name"`
	Module     string      `json:"module"` // e.g. "cloudflare:sockets"; "" for global
	Doc        string      `json:"doc"`
	TypeParams []TypeParam `json:"typeParams"`
	Extends    []Type      `json:"extends,omitempty"` // interface/class only
	Members    []Member    `json:"members,omitempty"` // interface/class only
	Type       *Type       `json:"type,omitempty"`    // alias only
}

// Member is a member of an interface or class, or of an inline object type
// literal.
type Member struct {
	Kind string `json:"kind"` // "property" | "method" | "index" | "getter" | "ctor"

	// property / method / getter
	Name string `json:"name,omitempty"`
	Doc  string `json:"doc,omitempty"`

	// property / getter
	Type     *Type `json:"type,omitempty"`
	Optional bool  `json:"optional,omitempty"`
	Readonly bool  `json:"readonly,omitempty"`

	// method / ctor
	Params     []Param     `json:"params,omitempty"`
	Returns    *Type       `json:"returns,omitempty"`
	TypeParams []TypeParam `json:"typeParams,omitempty"`

	// index
	KeyType   *Type `json:"keyType,omitempty"`
	ValueType *Type `json:"valueType,omitempty"`
}

// Param is a single method or constructor parameter.
type Param struct {
	Name     string `json:"name"`
	Type     *Type  `json:"type"`
	Optional bool   `json:"optional"`
	Rest     bool   `json:"rest"`
}

// Type is a TypeScript type, discriminated by K. Only the fields relevant
// to K are populated; see tmp/06-codegen-spec.md section 1.2.
type Type struct {
	K string `json:"k"`

	// prim, ref, typeParam
	Name string `json:"name,omitempty"`

	// ref
	Args []Type `json:"args,omitempty"`

	// literal
	Value any `json:"value,omitempty"`

	// array
	Elem *Type `json:"elem,omitempty"`

	// tuple
	Elems []Type `json:"elems,omitempty"`

	// union, intersection
	Types []Type `json:"types,omitempty"`

	// object
	Members []Member `json:"members,omitempty"`

	// function
	Params  []Param `json:"params,omitempty"`
	Returns *Type   `json:"returns,omitempty"`

	// unsupported
	Text string `json:"text,omitempty"`
}

// IsStringLiteral reports whether t is a literal type holding a string
// value (as opposed to a numeric or boolean literal).
func (t *Type) IsStringLiteral() bool {
	if t == nil || t.K != "literal" {
		return false
	}
	_, ok := t.Value.(string)
	return ok
}

// StringValue returns the literal's string value. It panics if the literal
// is not a string; callers should guard with IsStringLiteral.
func (t *Type) StringValue() string {
	return t.Value.(string)
}
