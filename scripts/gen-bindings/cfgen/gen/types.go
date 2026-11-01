package gen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/syumai/workers-go/scripts/gen-bindings/cfgen/ir"
)

// DeclKind is the generation strategy chosen for a declaration, per
// tmp/06-codegen-spec.md section 1.3 "型の分類".
type DeclKind int

const (
	KindHandle DeclKind = iota
	KindData
	KindAliasEnum
	KindAliasData
	KindAliasType
)

// classify determines how a declaration should be generated. declByName is
// the full IR's declaration index (not just the current package's include
// list), since resolving an intersection/extends composition may need to
// look through declarations that aren't themselves generated standalone.
func classify(declByName map[string]*ir.Decl, d *ir.Decl) DeclKind {
	if d.Kind == "alias" {
		t := d.Type
		if t == nil {
			return KindAliasType
		}
		if t.K == "union" && len(t.Types) > 0 && allStringLiterals(t.Types) {
			return KindAliasEnum
		}
		if t.K == "object" {
			return KindAliasData
		}
		if t.K == "intersection" || t.K == "union" {
			if _, ok := resolveDataMembers(declByName, d); ok {
				return KindAliasData
			}
		}
		return KindAliasType
	}
	for _, m := range d.Members {
		if m.Kind == "method" || m.Kind == "getter" {
			return KindHandle
		}
	}
	return KindData
}

func allStringLiterals(types []ir.Type) bool {
	for i := range types {
		if !types[i].IsStringLiteral() {
			return false
		}
	}
	return true
}

// exprConv describes how to convert a single Go value to/from its JS
// representation.
type exprConv struct {
	GoType string

	// FromJS returns Go statements assigning the (already declared,
	// addressable) Go expression dst from the js.Value expression src. If
	// the conversion is fallible, generated statements may
	// `return failReturn, err` on failure; failReturn must be a valid Go
	// expression for the enclosing function's non-error return value(s).
	FromJS func(dst, src, failReturn string) []string

	// ToJS returns any statements needed to compute the JS representation
	// of the Go expression src, plus the final js.Value-compatible
	// expression (which may simply be src itself for scalar types, since
	// js.Value.Set/SetIndex accept any type js.ValueOf supports).
	ToJS func(src string) (pre []string, expr string)

	// ZeroExpr is a Go expression for this type's zero value, used as the
	// failReturn placeholder by callers and (for data types) as the
	// zero-value literal for the enclosing function's own type.
	ZeroExpr string

	// OmitIfZero returns a boolean Go expression that is true when expr
	// should be *included* in the generated JS object (i.e. it is false
	// when the value is the type's zero value and should be omitted).
	OmitIfZero func(expr string) string

	// SelfGuarded reports whether FromJS already guards against a
	// null/undefined src internally (nilGuardWrap, pointerWrap): callers
	// that would otherwise wrap the call in their own
	// "if !s.IsUndefined() && !s.IsNull()" guard (genDataFromJS) can skip
	// it and call FromJS unconditionally instead.
	SelfGuarded bool

	// IsDataStruct reports whether this conversion's Go type is a plain
	// (non-pointer) struct for a data-shaped declaration — set only by
	// declRefConv's KindData/KindAliasData branch (directly, via a
	// "types:" override naming an included data declaration, or via
	// synthesizeDataType for an inline object/union, tmp/06-codegen-spec.md
	// 5.1 item 1). fieldConv uses it (isDataStructConv) to decide whether
	// an optional/nullable field of this type should be pointer-wrapped;
	// it exists as an explicit flag rather than being inferred from
	// ZeroExpr's shape because a coincidentally-identical-looking
	// ZeroExpr ("time.Time{}" for dateConv, matching the "<GoType>{}"
	// pattern declRefConv also happens to use) would otherwise cause a
	// plain built-in-typed field like R2HTTPMetadata.cacheExpiry
	// (time.Time) to be misidentified as a nested data struct and
	// pointer-wrapped into *time.Time — breaking any hand-written L2 code
	// that assumes the plain (unwrapped) struct-conversion precedent
	// (e.g. cloudflare/r2's HTTPMetadata(opts.HTTPMetadata) struct-to-
	// struct Go conversion, which requires both structs' fields to match
	// exactly).
	IsDataStruct bool
}

func scalarConv(goType, fromJS, zero string) exprConv {
	return exprConv{
		GoType:     goType,
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = " + src + fromJS} },
		ToJS:       func(src string) ([]string, string) { return nil, src },
		ZeroExpr:   zero,
		OmitIfZero: func(expr string) string { return expr + " != " + zero },
	}
}

func boolConv() exprConv {
	c := scalarConv("bool", ".Bool()", "false")
	c.OmitIfZero = func(expr string) string { return expr }
	return c
}

func jsValueConv() exprConv {
	return exprConv{
		GoType:     "js.Value",
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = " + src} },
		ToJS:       func(src string) ([]string, string) { return nil, src },
		ZeroExpr:   "js.Undefined()",
		OmitIfZero: func(expr string) string { return "!jsrt.IsNil(" + expr + ")" },
	}
}

// Package is the code-generation context for a single overrides file
// (i.e. a single output package).
type Package struct {
	IR         *ir.IR
	Ov         *Overrides
	declByName map[string]*ir.Decl
	included   map[string]*ir.Decl
	imports    map[string]bool
	warnings   []string

	// curDeclTypeParams and curMethodTypeParams are the typeParams in
	// scope while generating the declaration (and, if applicable, the
	// specific overload) currently being emitted; see typeParamType and
	// tmp/06-codegen-spec.md 2.1 item 2. Method-level params shadow
	// decl-level ones of the same name.
	curDeclTypeParams   []ir.TypeParam
	curMethodTypeParams []ir.TypeParam

	// curInParamPosition is true while resolving the conversion for a
	// method parameter's own type (set by convForParam), and false while
	// resolving a getter/property or method return type. It lets refConv
	// pick a direction-appropriate mapping for stream types, per
	// tmp/06-codegen-spec.md 3.1.1: a ReadableStream *parameter* maps to
	// io.Reader (values flow Go -> JS, via jsrt.ReadableStreamFromReader),
	// while a ReadableStream *return/property* maps to io.ReadCloser
	// (values flow JS -> Go) as before. It propagates through any nested
	// convFor call reached from a parameter's own type (array/record
	// element, union branch, ...), which is the desired behavior since
	// those nested values flow in the same direction as the parameter
	// itself.
	curInParamPosition bool

	// curContainerDepth counts how many levels of array/record wrapping
	// are currently being resolved: arrayConv and recordConv each bump it
	// around their own recursive convFor call for the element/value type,
	// so a nested container's own level (e.g. the inner []T of a
	// [][]T, or the value type of a Record<string, []T>) is one deeper
	// than its immediate parent's. arrayOfConv/recordConv use it (via
	// containerSuffix) to give each nesting level distinct loop/temp
	// variable names, so an inner loop can't shadow (and silently
	// corrupt) an outer one's index/element/key variable — see
	// containerSuffix's doc comment.
	curContainerDepth int

	// curDeclName and curMemberName track which declaration/member is
	// currently being generated, for two purposes: typeParamOverride's
	// "Decl.Param" override lookup (tmp/06-codegen-spec.md 5.1 item 3),
	// and giving warnf-adjacent messages (see refConv's include-list
	// warnings) enough context to triage without re-deriving it by hand.
	// curMemberName is "" while resolving a declaration's own shape (e.g.
	// its extends list) rather than one specific member.
	curDeclName   string
	curMemberName string

	// curNameHint is the Go type name to give an inline object type
	// literal (or an inline union that merges into one), if one is
	// encountered while resolving the type currently in scope — see
	// synthesizeDataType and tmp/06-codegen-spec.md 5.1 item 1. Set by
	// convForNamed/convForParamNamed around a field/param/return type's
	// own convFor call; propagates unchanged through array/record/union
	// wrapping so an inline object nested inside those still resolves to
	// the same name as its immediate field/param/return.
	curNameHint string

	// synthesized holds the data-shaped declarations manufactured for
	// inline object type literals / inline data-shaped unions, in
	// creation order; Generate emits one type for each (see
	// synthesizeDataType). synthesizedByHint memoizes by curNameHint so
	// resolving the same field's type more than once (genData's struct
	// fields vs. its fromJS/toJS bodies all call fieldConv independently)
	// reuses one synthesized declaration instead of emitting duplicates.
	synthesized       []*ir.Decl
	synthesizedByHint map[string]*ir.Decl

	// pendingNotes accumulates notef messages raised while resolving the
	// type(s) for the field/getter/method currently being emitted (reset
	// by resetNotes, drained by takeNotes); the caller folds them into
	// that member's doc comment, per tmp/06-codegen-spec.md 5.1 item 2's
	// "選ばれた枝を doc comment に書く".
	pendingNotes []string

	// infos are non-actionable, informational messages (e.g. a union
	// resolved unambiguously to a single in-include declaration) that are
	// reported separately from warnings: unlike a warning, they don't
	// indicate a fallback to js.Value that a human should consider fixing.
	// infoSeen dedupes infos: a data type's field conversion is resolved
	// independently up to three times (its struct field, fromJS, and
	// toJS each call fieldConv), which would otherwise repeat the exact
	// same notef message that many times.
	infos    []string
	infoSeen map[string]bool

	// usedGoNames records every already-resolved declaration's final Go
	// type name (both real included declarations, populated up front by
	// NewPackage, and each synthesized one as synthesizeDataType creates
	// it), so a newly computed name-hint that would collide with an
	// unrelated existing type can be disambiguated instead of silently
	// producing two Go declarations with the same name. This does happen
	// in practice: e.g. cf's flattened IncomingRequestCfProperties.
	// botManagement field's type, after extends-flattening picks up the
	// Enterprise variant's inline intersection, hints to the same Go name
	// ("IncomingRequestCFPropertiesBotManagement") as the already-included
	// standalone IncomingRequestCfPropertiesBotManagement interface.
	usedGoNames map[string]bool
}

// containerSuffix returns the loop/temp-variable name suffix for a
// container conversion (array or record) at nesting depth d: "" for the
// outermost level, so a non-nested slice/map's generated code is unchanged
// from before nested-container naming existed (no golden-file churn for the
// common case), and the depth's decimal digits for any deeper level (e.g.
// "1", "2", ...). Without per-depth suffixes, arrayOfConv's inner "for i :=
// range dst[i]" (built while generating a [][]T's inner slice conversion)
// shadows the outer loop's own "i", so the inner loop body's "dst[i][i]"
// reads and writes the wrong indices — the outer index is lost the moment
// the inner "i" comes into scope. The same shadowing corrupts any other
// combination of directly-nested arrayConv/recordConv (Record<string,
// []T>, []Record<string, T>, ...), since the inner container's FromJS/ToJS
// closures are spliced directly into the outer container's own loop body.
func containerSuffix(depth int) string {
	if depth == 0 {
		return ""
	}
	return strconv.Itoa(depth)
}

func NewPackage(doc *ir.IR, ov *Overrides) *Package {
	p := &Package{
		IR:                doc,
		Ov:                ov,
		declByName:        indexDecls(doc),
		included:          map[string]*ir.Decl{},
		imports:           map[string]bool{},
		synthesizedByHint: map[string]*ir.Decl{},
		infoSeen:          map[string]bool{},
		usedGoNames:       map[string]bool{},
	}
	for _, name := range ov.Include {
		if d, ok := p.declByName[name]; ok {
			p.included[name] = d
		}
	}
	for _, d := range p.included {
		p.usedGoNames[p.declGoName(d)] = true
	}
	return p
}

func (p *Package) warnf(format string, args ...any) {
	p.warnings = append(p.warnings, fmt.Sprintf(format, args...))
}

// warnCtxf is warnf, but prefixed with "Decl" or "Decl.member" context from
// curDeclName/curMemberName when available (set by genDecl/genGetter/
// fieldConv/genMethodGroup), so a triage pass over cfgen's warning output
// doesn't have to re-derive which declaration/member a fallback came from.
func (p *Package) warnCtxf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	switch {
	case p.curDeclName != "" && p.curMemberName != "":
		msg = p.curDeclName + "." + p.curMemberName + ": " + msg
	case p.curDeclName != "":
		msg = p.curDeclName + ": " + msg
	}
	p.warnings = append(p.warnings, msg)
}

// notef records an informational message: something cfgen resolved
// automatically (e.g. a union with exactly one in-include ref member) that
// a human doesn't need to act on, as opposed to a warnf fallback to
// js.Value. It both appends to Infos() (reported separately from
// Warnings(), so it isn't counted as a warning) and queues onto
// pendingNotes so the caller can fold it into the affected field/method's
// generated doc comment.
func (p *Package) notef(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if !p.infoSeen[msg] {
		p.infoSeen[msg] = true
		p.infos = append(p.infos, msg)
	}
	p.pendingNotes = append(p.pendingNotes, msg)
}

// resetNotes clears pendingNotes; callers emitting one field/getter/method's
// doc comment call it before resolving that member's type(s), then
// takeNotes after, so pendingNotes never leaks notes from a previously
// emitted member into this one's doc comment.
func (p *Package) resetNotes() { p.pendingNotes = nil }

// takeNotes drains and returns pendingNotes.
func (p *Package) takeNotes() []string {
	notes := p.pendingNotes
	p.pendingNotes = nil
	return notes
}

// appendNotesToDoc appends notes (if any) to doc as trailing lines,
// separated by a blank line from doc's own text.
func appendNotesToDoc(doc string, notes []string) string {
	if len(notes) == 0 {
		return doc
	}
	var sb strings.Builder
	sb.WriteString(doc)
	if doc != "" {
		sb.WriteString("\n\n")
	}
	for i, n := range notes {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(n)
	}
	return sb.String()
}

// Warnings returns the warnings collected during generation.
func (p *Package) Warnings() []string { return p.warnings }

// Infos returns the informational (non-warning) messages collected during
// generation; see notef.
func (p *Package) Infos() []string { return p.infos }

func (p *Package) useImport(path string) { p.imports[path] = true }

func (p *Package) isBinding(declName string) bool {
	for _, n := range p.Ov.Bindings {
		if n == declName {
			return true
		}
	}
	return false
}

func memberKey(declName, member string) string { return declName + "." + member }

func (p *Package) isHandwritten(declName, member string) bool {
	return containsAny(p.Ov.Handwritten, declName, memberKey(declName, member))
}

func (p *Package) isExcluded(declName, member string) bool {
	return containsAny(p.Ov.Exclude, declName, memberKey(declName, member))
}

// isForcedOptional reports whether overrides mark declName.member optional
// even though the IR says it is required. Only the "Decl.member" key
// matches: a bare declaration name would make every field optional.
func (p *Package) isForcedOptional(declName, member string) bool {
	return containsAny(p.Ov.Optional, memberKey(declName, member))
}

func containsAny(list []string, candidates ...string) bool {
	for _, item := range list {
		for _, c := range candidates {
			if item == c {
				return true
			}
		}
	}
	return false
}

// declGoName resolves the exported Go identifier for a declaration,
// honoring a whole-declaration rename override.
func (p *Package) declGoName(d *ir.Decl) string {
	if r, ok := p.Ov.Rename[d.Name]; ok {
		return r
	}
	base := d.Name
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[i+1:]
	}
	return exportedName(base)
}

// memberName resolves the exported Go name for a member (field or method),
// honoring a "Decl.member" rename override.
func (p *Package) memberName(declName, member string) string {
	if r, ok := p.Ov.Rename[memberKey(declName, member)]; ok {
		return r
	}
	return exportedName(member)
}

// paramGoName resolves the unexported Go identifier for a method parameter,
// honoring a "Decl.method.<rawParamName>" rename override (same Rename map
// as declGoName/memberName, keyed one level deeper). This exists for
// parameters whose extracted name isn't a valid Go identifier at all — e.g.
// a .d.ts destructured-object parameter's "name" is the raw destructuring
// pattern source text ("{\n  type,\n  payload,\n}"), not an identifier
// (tmp/06-codegen-spec.md 6.2 item 1, WorkflowInstance.sendEvent). Falls
// back to goParamName(paramName) when no override matches, same as before
// this existed.
func (p *Package) paramGoName(declName, member, paramName string) string {
	key := memberKey(declName, member) + "." + paramName
	if r, ok := p.Ov.Rename[key]; ok {
		return r
	}
	return goParamName(paramName)
}

// typeOverride looks up a "types:" override. suffix is "" for a property,
// "returns" for a method return type, or "params.<name>" for a method
// parameter type.
func (p *Package) typeOverride(declName, member, suffix string) string {
	key := memberKey(declName, member)
	if suffix != "" {
		key += "." + suffix
	}
	return p.Ov.Types[key]
}

// convForParam is convFor for a method parameter's own type: it sets
// curInParamPosition around the call (restoring the previous value
// afterward, so a nested call from within a data type's field/method
// resolution isn't accidentally left in "param" mode) so refConv can pick
// the argument-direction mapping for stream types (ReadableStream ->
// io.Reader, WritableStream -> js.Value) instead of the
// return/property-direction one. hint is the name to give an inline object
// type literal encountered while resolving t (tmp/06-codegen-spec.md 5.1
// item 1); pass "" where none is available (informational-only positions).
func (p *Package) convForParam(t *ir.Type, override, hint string) (exprConv, error) {
	prev := p.curInParamPosition
	p.curInParamPosition = true
	prevHint := p.curNameHint
	p.curNameHint = hint
	defer func() { p.curInParamPosition = prev; p.curNameHint = prevHint }()
	return p.convFor(t, override)
}

// convForNamed is convFor for a property/getter/return type's own type
// (i.e. not a method parameter): it sets curNameHint the same way
// convForParam does, without touching curInParamPosition.
func (p *Package) convForNamed(t *ir.Type, override, hint string) (exprConv, error) {
	prevHint := p.curNameHint
	p.curNameHint = hint
	defer func() { p.curNameHint = prevHint }()
	return p.convFor(t, override)
}

// convFor resolves the Go conversion for an IR type, applying override (a
// "types:" Go type string) when non-empty.
func (p *Package) convFor(t *ir.Type, override string) (exprConv, error) {
	if override != "" {
		return p.convForOverride(t, override)
	}
	if t == nil {
		return jsValueConv(), nil
	}
	switch t.K {
	case "prim":
		return p.primConv(t.Name), nil
	case "literal":
		return literalConv(t), nil
	case "array":
		return p.arrayConv(t.Elem)
	case "tuple":
		p.warnf("tuple types are not supported, falling back to js.Value")
		return jsValueConv(), nil
	case "ref":
		return p.refConv(t)
	case "union":
		return p.unionConv(t)
	case "intersection":
		if members, ok := resolveDataMembers(p.declByName, &ir.Decl{Kind: "alias", Type: t}); ok {
			return p.synthesizeDataType(members)
		}
		p.warnCtxf("intersection types are not supported, falling back to js.Value")
		return jsValueConv(), nil
	case "object":
		return p.synthesizeDataType(t.Members)
	case "function":
		p.warnCtxf("function types are not supported, falling back to js.Value")
		return jsValueConv(), nil
	case "typeParam":
		if ov := p.typeParamOverride(t.Name); ov != "" {
			return p.convForOverride(t, ov)
		}
		// A typeParam with no default, or overridden explicitly, falls
		// back to js.Value silently (tmp/06-codegen-spec.md 5.1 item 3):
		// generics erasure at this boundary is expected and unavoidable
		// (a Go method can't itself be generic over the caller's choice
		// of T the way the TypeScript declaration is), not something a
		// human needs to act on.
		if def := p.typeParamDefault(t.Name); def != nil {
			return p.convFor(def, "")
		}
		return jsValueConv(), nil
	case "unsupported":
		p.warnf("unsupported TypeScript construct %q, falling back to js.Value", t.Text)
		return jsValueConv(), nil
	}
	p.warnf("unrecognized IR type kind %q, falling back to js.Value", t.K)
	return jsValueConv(), nil
}

// typeParamDefault resolves the default type bound to a typeParam named
// name, per tmp/06-codegen-spec.md 2.1 item 2: the method's own typeParams
// (if it has one by this name) take precedence over the enclosing
// declaration's. Returns nil if name isn't in scope, or is in scope but has
// no default (both cases fall back to js.Value, per the spec table).
func (p *Package) typeParamDefault(name string) *ir.Type {
	for _, tp := range p.curMethodTypeParams {
		if tp.Name == name {
			return tp.Default
		}
	}
	for _, tp := range p.curDeclTypeParams {
		if tp.Name == name {
			return tp.Default
		}
	}
	return nil
}

// typeParamOverride looks up a "typeParams:" override (tmp/06-codegen-spec.md
// 5.1 item 3) for type parameter name in scope of the declaration currently
// being generated (curDeclName), keyed "Decl.Param". Returns "" if none is
// configured.
func (p *Package) typeParamOverride(name string) string {
	if p.curDeclName == "" {
		return ""
	}
	return p.Ov.TypeParams[p.curDeclName+"."+name]
}

// synthesizeDataType manufactures (or, if one was already made for the same
// curNameHint, reuses) a data-shaped declaration for an inline object type
// literal or an inline data-shaped union (tmp/06-codegen-spec.md 5.1 item
// 1), and returns the conversion for it — identical in shape to a
// convFor("ref"-to-an-included-data-type) conversion. members is the
// already-flattened/merged property list (from the object literal directly,
// or from resolveUnionMembers/resolveDataMembers for a union/intersection).
// If no name hint is in scope (curNameHint == ""; shouldn't happen for any
// currently generated package, since every call site that can reach an
// inline object/union sets one), falls back to js.Value with a warning
// rather than generating an unnamed type.
func (p *Package) synthesizeDataType(members []ir.Member) (exprConv, error) {
	hint := p.curNameHint
	if hint == "" {
		p.warnCtxf("inline object/union type literal has no name hint in this position, falling back to js.Value")
		return jsValueConv(), nil
	}
	d, ok := p.synthesizedByHint[hint]
	if !ok {
		name := p.uniqueSynthesizedName(hint)
		d = &ir.Decl{Kind: "interface", Name: name, Members: members}
		// Memoized by the original hint (not the disambiguated name), so
		// a repeat resolution of the very same field (genData's struct
		// fields vs. its fromJS/toJS bodies) reuses this declaration
		// instead of colliding with — and being disambiguated away
		// from — itself on the second call.
		p.synthesizedByHint[hint] = d
		p.included[name] = d
		p.synthesized = append(p.synthesized, d)
		p.usedGoNames[p.declGoName(d)] = true
	}
	return p.declRefConv(d)
}

// uniqueSynthesizedName returns hint, or — if hint's own computed Go name
// collides with an already-used one (see usedGoNames) — hint with a "2",
// "3", ... suffix appended until the resulting Go name is unique.
func (p *Package) uniqueSynthesizedName(hint string) string {
	if !p.usedGoNames[p.declGoName(&ir.Decl{Name: hint})] {
		return hint
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s%d", hint, i)
		if !p.usedGoNames[p.declGoName(&ir.Decl{Name: candidate})] {
			return candidate
		}
	}
}

func (p *Package) primConv(name string) exprConv {
	switch name {
	case "string":
		return scalarConv("string", ".String()", `""`)
	case "boolean":
		return boolConv()
	case "number":
		return scalarConv("float64", ".Float()", "0")
	case "bigint":
		c := scalarConv("int64", ".Int()", "0")
		c.FromJS = func(dst, src, _ string) []string { return []string{dst + " = int64(" + src + ".Int())"} }
		return c
	case "any", "unknown", "object":
		return jsValueConv()
	default:
		p.warnf("unsupported primitive type %q, falling back to js.Value", name)
		return jsValueConv()
	}
}

func literalConv(t *ir.Type) exprConv {
	switch t.Value.(type) {
	case string:
		return scalarConv("string", ".String()", `""`)
	case bool:
		return boolConv()
	case float64:
		return scalarConv("float64", ".Float()", "0")
	default:
		return jsValueConv()
	}
}

func (p *Package) arrayConv(elem *ir.Type) (exprConv, error) {
	if elem == nil {
		elem = &ir.Type{K: "prim", Name: "any"}
	}
	depth := p.curContainerDepth
	p.curContainerDepth++
	ec, err := p.convFor(elem, "")
	p.curContainerDepth = depth
	if err != nil {
		return exprConv{}, err
	}
	return arrayOfConv(ec, depth), nil
}

// arrayOfConv builds a plain-JS-Array <-> Go slice conversion given the
// element conversion and this array's own nesting depth (see
// curContainerDepth/containerSuffix). Element conversions that themselves
// require pre-statements in ToJS are not supported (not needed by the
// current generation targets) and fall back with a warning handled by the
// caller.
func arrayOfConv(elem exprConv, depth int) exprConv {
	goType := "[]" + elem.GoType
	suf := containerSuffix(depth)
	idxVar, elemVar, arrVar := "i"+suf, "e"+suf, "arr"+suf
	return exprConv{
		GoType: goType,
		FromJS: func(dst, src, failReturn string) []string {
			lines := []string{
				dst + " = make(" + goType + ", " + src + ".Length())",
				"for " + idxVar + " := range " + dst + " {",
			}
			inner := elem.FromJS(dst+"["+idxVar+"]", src+".Index("+idxVar+")", failReturn)
			lines = append(lines, indentAll(inner)...)
			lines = append(lines, "}")
			return lines
		},
		ToJS: func(src string) ([]string, string) {
			pre, elemExpr := elem.ToJS(elemVar)
			var lines []string
			lines = append(lines, arrVar+" := js.Global().Get(\"Array\").New(len("+src+"))")
			lines = append(lines, "for "+idxVar+", "+elemVar+" := range "+src+" {")
			if len(pre) > 0 {
				lines = append(lines, indentAll(pre)...)
			}
			lines = append(lines, "\t"+arrVar+".SetIndex("+idxVar+", "+elemExpr+")")
			lines = append(lines, "}")
			return lines, arrVar
		},
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return "len(" + expr + ") > 0" },
	}
}

// recordConv builds a plain-JS-object <-> Go map conversion for
// Record<string, val>, using curContainerDepth/containerSuffix (the same
// scheme as arrayOfConv) to give each nesting level of a Record-of-Record,
// Record-of-array, array-of-Record, ... distinct loop/temp variable names.
func (p *Package) recordConv(val *ir.Type) (exprConv, error) {
	depth := p.curContainerDepth
	p.curContainerDepth++
	vc, err := p.convFor(val, "")
	p.curContainerDepth = depth
	if err != nil {
		return exprConv{}, err
	}
	goType := "map[string]" + vc.GoType
	suf := containerSuffix(depth)
	keysVar, idxVar, keyVar, valVar, mapVar, rangeValVar := "keys"+suf, "i"+suf, "k"+suf, "vv"+suf, "m"+suf, "v"+suf
	return exprConv{
		GoType: goType,
		FromJS: func(dst, src, failReturn string) []string {
			lines := []string{
				dst + " = make(" + goType + ")",
				keysVar + " := js.Global().Get(\"Object\").Call(\"keys\", " + src + ")",
				"for " + idxVar + " := 0; " + idxVar + " < " + keysVar + ".Length(); " + idxVar + "++ {",
				"\t" + keyVar + " := " + keysVar + ".Index(" + idxVar + ").String()",
			}
			lines = append(lines, "\tvar "+valVar+" "+vc.GoType)
			inner := vc.FromJS(valVar, src+".Get("+keyVar+")", failReturn)
			lines = append(lines, indentAll(inner)...)
			lines = append(lines, "\t"+dst+"["+keyVar+"] = "+valVar)
			lines = append(lines, "}")
			return lines
		},
		ToJS: func(src string) ([]string, string) {
			pre, valExpr := vc.ToJS(rangeValVar)
			lines := []string{
				mapVar + " := jsrt.NewObject()",
				"for " + keyVar + ", " + rangeValVar + " := range " + src + " {",
			}
			lines = append(lines, indentAll(pre)...)
			lines = append(lines, "\t"+mapVar+".Set("+keyVar+", "+valExpr+")")
			lines = append(lines, "}")
			return lines, mapVar
		},
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return "len(" + expr + ") > 0" },
	}, nil
}

// mapConv builds a JS Map<string, V> <-> Go map[string]V conversion, per
// tmp/06-codegen-spec.md 4.1: FromJS iterates Array.from(v.keys()) and reads
// each value with v.get(k) (a JS Map, unlike a Record/plain object, doesn't
// support Object.keys/property-get); ToJS is provided for symmetry (no
// generated package currently needs it) using the JS Map constructor and
// .set(). Uses curContainerDepth/containerSuffix like arrayOfConv/recordConv
// so a Map nested inside another container gets distinct loop/temp variable
// names.
func (p *Package) mapConv(val *ir.Type) (exprConv, error) {
	depth := p.curContainerDepth
	p.curContainerDepth++
	// tmp/06-codegen-spec.md 5.1 item 6: Map<string, T | null> (e.g. the
	// KV bulk-get overloads) needs its own "value absent" representation
	// per entry — a *T for a prim T (nullableReturnConv pointer-wraps
	// it), or an untouched js.Value when T itself isn't resolvable (a
	// defaultless type parameter, per item 3) since js.Value already
	// round-trips null/undefined on its own.
	target, nullable := splitNullable(val)
	vc, err := p.convFor(target, "")
	p.curContainerDepth = depth
	if err != nil {
		return exprConv{}, err
	}
	if nullable {
		vc = p.nullableReturnConv(vc)
	}
	goType := "map[string]" + vc.GoType
	suf := containerSuffix(depth)
	// Named distinctly from arrayOfConv/recordConv's "keys"/"i"/"k"/... (and
	// prefixed so they can't collide with a real Go parameter name in the
	// same function, e.g. DurableObjectStorage.GetMultiple's own "keys"
	// parameter, which a plain "keys" here would shadow-redeclare into a
	// "no new variables on left side of :=" compile error).
	keysVar, idxVar, keyVar, valVar, mapVar, rangeValVar := "mapKeys"+suf, "mapIdx"+suf, "mapKey"+suf, "mapVal"+suf, "mapObj"+suf, "mapRV"+suf
	return exprConv{
		GoType: goType,
		FromJS: func(dst, src, failReturn string) []string {
			lines := []string{
				dst + " = make(" + goType + ")",
				keysVar + " := js.Global().Get(\"Array\").Call(\"from\", " + src + ".Call(\"keys\"))",
				"for " + idxVar + " := 0; " + idxVar + " < " + keysVar + ".Length(); " + idxVar + "++ {",
				"\t" + keyVar + " := " + keysVar + ".Index(" + idxVar + ").String()",
			}
			lines = append(lines, "\tvar "+valVar+" "+vc.GoType)
			inner := vc.FromJS(valVar, src+".Call(\"get\", "+keyVar+")", failReturn)
			lines = append(lines, indentAll(inner)...)
			lines = append(lines, "\t"+dst+"["+keyVar+"] = "+valVar)
			lines = append(lines, "}")
			return lines
		},
		ToJS: func(src string) ([]string, string) {
			pre, valExpr := vc.ToJS(rangeValVar)
			lines := []string{
				mapVar + " := js.Global().Get(\"Map\").New()",
				"for " + keyVar + ", " + rangeValVar + " := range " + src + " {",
			}
			lines = append(lines, indentAll(pre)...)
			lines = append(lines, "\t"+mapVar+".Call(\"set\", "+keyVar+", "+valExpr+")")
			lines = append(lines, "}")
			return lines, mapVar
		},
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return "len(" + expr + ") > 0" },
	}, nil
}

func bytesConv() exprConv {
	return exprConv{
		GoType:     "[]byte",
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = jsrt.BytesFromJS(" + src + ")"} },
		ToJS:       func(src string) ([]string, string) { return nil, "jsrt.BytesToJS(" + src + ")" },
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return "len(" + expr + ") > 0" },
	}
}

func float32ArrayConv() exprConv {
	return exprConv{
		GoType:     "[]float32",
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = jsrt.Float32ArrayFromJS(" + src + ")"} },
		ToJS:       func(src string) ([]string, string) { return nil, "jsrt.Float32ArrayToJS(" + src + ")" },
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return "len(" + expr + ") > 0" },
	}
}

func float64ArrayConv() exprConv {
	return exprConv{
		GoType:     "[]float64",
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = jsrt.Float64ArrayFromJS(" + src + ")"} },
		ToJS:       func(src string) ([]string, string) { return nil, "jsrt.Float64ArrayToJS(" + src + ")" },
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return "len(" + expr + ") > 0" },
	}
}

func dateConv() exprConv {
	return exprConv{
		GoType:     "time.Time",
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = jsrt.DateToTime(" + src + ")"} },
		ToJS:       func(src string) ([]string, string) { return nil, "jsrt.TimeToDate(" + src + ")" },
		ZeroExpr:   "time.Time{}",
		OmitIfZero: func(expr string) string { return "!" + expr + ".IsZero()" },
	}
}

func headersConv() exprConv {
	return exprConv{
		GoType:     "http.Header",
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = jsrt.HeadersFromJS(" + src + ")"} },
		ToJS:       func(src string) ([]string, string) { return nil, "jsrt.HeadersToJS(" + src + ")" },
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return "len(" + expr + ") > 0" },
	}
}

func readCloserConv() exprConv {
	return exprConv{
		GoType:     "io.ReadCloser",
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = jsrt.ReadCloser(" + src + ")"} },
		ToJS:       func(src string) ([]string, string) { return nil, src },
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return expr + " != nil" },
	}
}

// readerParamConv is the argument-position mapping for a ReadableStream<...>
// type (tmp/06-codegen-spec.md 3.1.1): the Go parameter is an io.Reader, and
// ToJS converts it to a JS ReadableStream via jsrt.ReadableStreamFromReader.
// FromJS is provided for completeness (a data-type field reached only from a
// parameter's own type could in principle need it) and mirrors
// readCloserConv's.
func readerParamConv() exprConv {
	return exprConv{
		GoType:     "io.Reader",
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = jsrt.ReadCloser(" + src + ")"} },
		ToJS:       func(src string) ([]string, string) { return nil, "jsrt.ReadableStreamFromReader(" + src + ")" },
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return expr + " != nil" },
	}
}

// writeCloserConv is the return/property-position mapping for a
// WritableStream type (tmp/06-codegen-spec.md 3.1.1): FromJS wraps the JS
// value as an io.WriteCloser via jsrt.WriteCloser.
func writeCloserConv() exprConv {
	return exprConv{
		GoType:     "io.WriteCloser",
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = jsrt.WriteCloser(" + src + ")"} },
		ToJS:       func(src string) ([]string, string) { return nil, src },
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return expr + " != nil" },
	}
}

func (p *Package) refConv(t *ir.Type) (exprConv, error) {
	switch t.Name {
	case "Array", "Iterable":
		// tmp/06-codegen-spec.md 2.1 item 7: Iterable<T> is treated exactly
		// like Array<T> (a plain JS Array round-trips as a Go slice either
		// way, and cfgen only ever needs to emit values, not consume
		// arbitrary iterables).
		var elem *ir.Type
		if len(t.Args) > 0 {
			elem = &t.Args[0]
		}
		return p.arrayConv(elem)
	case "Record":
		var val *ir.Type
		if len(t.Args) > 1 {
			val = &t.Args[1]
		} else {
			val = &ir.Type{K: "prim", Name: "any"}
		}
		p.useImport("jsrt")
		return p.recordConv(val)
	case "Map":
		// tmp/06-codegen-spec.md 4.1: Map<string, T> -> map[string]T (Map's
		// key type isn't checked; every generated use is Map<string, ...>).
		var val *ir.Type
		if len(t.Args) > 1 {
			val = &t.Args[1]
		} else {
			val = &ir.Type{K: "prim", Name: "any"}
		}
		return p.mapConv(val)
	case "ArrayBuffer", "Uint8Array":
		p.useImport("jsrt")
		return bytesConv(), nil
	case "Float32Array":
		p.useImport("jsrt")
		return float32ArrayConv(), nil
	case "Float64Array":
		p.useImport("jsrt")
		return float64ArrayConv(), nil
	case "Date":
		p.useImport("jsrt")
		p.useImport("time")
		return dateConv(), nil
	case "Headers":
		p.useImport("jsrt")
		p.useImport("net/http")
		return headersConv(), nil
	case "ReadableStream":
		p.useImport("jsrt")
		p.useImport("io")
		if p.curInParamPosition {
			return readerParamConv(), nil
		}
		return readCloserConv(), nil
	case "WritableStream":
		if p.curInParamPosition {
			// Argument-position WritableStream is left as a raw escape
			// hatch for now (tmp/06-codegen-spec.md 3.1.1: "引数の
			// WritableStream → js.Value（当面）"); no generated method in
			// the current packages actually takes one.
			return jsValueConv(), nil
		}
		p.useImport("jsrt")
		p.useImport("io")
		return writeCloserConv(), nil
	case "Request", "Response":
		// Left as a raw escape hatch; L2 idiomatic packages translate these.
		return jsValueConv(), nil
	case "Promise":
		p.warnf("unexpected Promise type outside of a method return position")
		return jsValueConv(), nil
	default:
		if d, ok := p.included[t.Name]; ok {
			return p.declRefConv(d)
		}
		if _, ok := p.declByName[t.Name]; ok {
			p.warnf("ref %q exists in the IR but is not in this package's include list; falling back to js.Value", t.Name)
		} else {
			p.warnf("ref %q was not found in the IR; falling back to js.Value", t.Name)
		}
		return jsValueConv(), nil
	}
}

func (p *Package) declRefConv(d *ir.Decl) (exprConv, error) {
	name := p.declGoName(d)
	switch classify(p.declByName, d) {
	case KindHandle:
		p.useImport("jsrt")
		return exprConv{
			GoType: "*" + name,
			FromJS: func(dst, src, _ string) []string { return []string{dst + " = " + name + "FromJS(" + src + ")"} },
			ToJS: func(src string) ([]string, string) {
				return nil, src + ".JSValue()"
			},
			ZeroExpr:   "nil",
			OmitIfZero: func(expr string) string { return expr + " != nil" },
		}, nil
	case KindData, KindAliasData:
		fromJSFunc := unexportedName(name) + "FromJS"
		return exprConv{
			GoType: name,
			FromJS: func(dst, src, failReturn string) []string {
				if failReturn == "" {
					// A getter's enclosing function returns a single
					// value, so there's nowhere to propagate a decode
					// error to; harmless in practice since a generated
					// data-type FromJS never actually returns a non-nil
					// error today. See genGetter.
					return []string{
						"if tmp, err := " + fromJSFunc + "(" + src + "); err == nil {",
						"\t" + dst + " = tmp",
						"}",
					}
				}
				return []string{
					"if tmp, err := " + fromJSFunc + "(" + src + "); err != nil {",
					"\treturn " + failReturn + ", err",
					"} else {",
					"\t" + dst + " = tmp",
					"}",
				}
			},
			ToJS:         func(src string) ([]string, string) { return nil, src + ".toJS()" },
			ZeroExpr:     name + "{}",
			OmitIfZero:   func(string) string { return "true" },
			IsDataStruct: true,
		}, nil
	case KindAliasEnum:
		return exprConv{
			GoType:     name,
			FromJS:     func(dst, src, _ string) []string { return []string{dst + " = " + name + "(" + src + ".String())"} },
			ToJS:       func(src string) ([]string, string) { return nil, "string(" + src + ")" },
			ZeroExpr:   `""`,
			OmitIfZero: func(expr string) string { return expr + ` != ""` },
		}, nil
	default: // KindAliasType: recurse into the underlying type, honoring a
		// types: override keyed by the alias's own declaration name (so it
		// applies consistently whether the alias is generated directly or
		// reached through a ref elsewhere).
		return p.convFor(d.Type, p.Ov.Types[d.Name])
	}
}

// isNullableType reports whether t is a union including a null or
// undefined variant (regardless of how many other variants remain), i.e.
// whether decoding it needs an undefined/null guard even when the IR
// doesn't mark the containing member itself "optional" (a TS property typed
// `foo: string | null`, as opposed to `foo?: string`).
func isNullableType(t *ir.Type) bool {
	if t == nil || t.K != "union" {
		return false
	}
	for _, mt := range t.Types {
		if mt.K == "prim" && (mt.Name == "null" || mt.Name == "undefined") {
			return true
		}
	}
	return false
}

// splitNullable is isNullableType plus, when there's exactly one non-null
// variant left after stripping null/undefined, that variant — used at
// method-return positions to decide the "T | null -> (*T, error)" mapping
// per tmp/06-codegen-spec.md 2.1 item 3. A multi-variant remainder (rare;
// not exercised by any generated package today) reports not-nullable here,
// leaving the whole union to fall back to convFor's ordinary (and, for a
// non-single remainder, warning) union handling.
func splitNullable(t *ir.Type) (*ir.Type, bool) {
	if !isNullableType(t) {
		return t, false
	}
	var nonNull []ir.Type
	for _, mt := range t.Types {
		if mt.K == "prim" && (mt.Name == "null" || mt.Name == "undefined") {
			continue
		}
		nonNull = append(nonNull, mt)
	}
	if len(nonNull) != 1 {
		return t, false
	}
	return &nonNull[0], true
}

// nullableReturnConv adapts conv (already resolved for nonNullType, the
// stripped non-null variant of a Promise<T | null/undefined> or sync
// T | null/undefined return type) to represent the "value absent" case
// explicitly, per tmp/06-codegen-spec.md 2.1 item 3:
//
//   - js.Value is left alone: js.Undefined()/null already round-trip
//     through it untouched, and callers use jsrt.IsNil to test it.
//   - A type whose zero value already unambiguously means "absent" (a
//     handle's *Name, []byte, io.ReadCloser, a map, a slice, ...; anything
//     with ZeroExpr "nil") just gets an added guard so a JS
//     null/undefined maps to that zero value instead of being handed to
//     the inner FromJS (which, for a handle type in particular, would
//     otherwise silently wrap the null value instead of reporting absence).
//   - Anything else (a prim scalar - string/number/boolean - or a data
//     struct) gets pointer-wrapped, since those types' zero values (""`,
//     0, false, an all-zero-fields struct) are ordinary, valid values and
//     so can't otherwise be told apart from "absent".
func (p *Package) nullableReturnConv(conv exprConv) exprConv {
	if conv.GoType == "js.Value" {
		return conv
	}
	p.useImport("jsrt")
	if conv.ZeroExpr == "nil" {
		return nilGuardWrap(conv)
	}
	return pointerWrap(conv)
}

// nilGuardWrap wraps inner's FromJS so that a JS null/undefined source
// short-circuits to inner's (already nil-ish) zero value instead of being
// passed through to inner.FromJS.
func nilGuardWrap(inner exprConv) exprConv {
	wrapped := inner
	wrapped.FromJS = func(dst, src, failReturn string) []string {
		lines := []string{"if !jsrt.IsNil(" + src + ") {"}
		lines = append(lines, indentAll(inner.FromJS(dst, src, failReturn))...)
		lines = append(lines, "}")
		return lines
	}
	wrapped.SelfGuarded = true
	return wrapped
}

// pointerWrap turns inner's Go type T into *T, so a JS null/undefined
// source can map to a nil pointer instead of T's (otherwise ambiguous,
// looks-like-a-real-value) zero value.
func pointerWrap(inner exprConv) exprConv {
	goType := "*" + inner.GoType
	return exprConv{
		GoType: goType,
		FromJS: func(dst, src, failReturn string) []string {
			lines := []string{"if !jsrt.IsNil(" + src + ") {", "\tvar val " + inner.GoType}
			lines = append(lines, indentAll(inner.FromJS("val", src, failReturn))...)
			lines = append(lines, "\t"+dst+" = &val", "}")
			return lines
		},
		ToJS: func(src string) ([]string, string) {
			// Parenthesized so a method-call ToJS (e.g. a nested data
			// type's "<expr>.toJS()") dereferences the pointer before
			// calling, not after: "(*src).toJS()", not "*src.toJS()"
			// (which - since .toJS() binds tighter than unary * - would
			// try to dereference the js.Value result instead).
			return inner.ToJS("(*" + src + ")")
		},
		ZeroExpr:    "nil",
		OmitIfZero:  func(expr string) string { return expr + " != nil" },
		SelfGuarded: true,
	}
}

// isDataStructConv reports whether conv is a plain (non-pointer) Go struct
// conversion for a data-shaped declaration — see exprConv.IsDataStruct.
func isDataStructConv(conv exprConv) bool {
	return conv.IsDataStruct
}

// fieldConv resolves the conversion for one data-type struct field
// (property member m of data-shaped declaration d), applying a "types:"
// override if present and, per tmp/06-codegen-spec.md 1.3's "data 型" rule,
// pointer-wrapping an optional-or-nullable field whose type resolves to a
// nested data-type struct — whether directly (`field?: Other` /
// `field: Other | null` / `field: Other | undefined`), via a "types:"
// override naming an included data declaration (e.g. R2GetOptions.onlyIf's
// `types: R2Conditional`, picking one branch of an `R2Conditional |
// Headers` union), or via an inline object/union type literal synthesized
// into its own data type (5.1 item 1) — into `*Other`, omitted entirely
// when nil in toJS and only allocated-and-decoded when present in fromJS.
// Without this, a plain (non-pointer) struct field can't tell its zero
// value apart from "the caller didn't set this", so toJS always sent it —
// e.g. R2GetOptions.range / R2PutOptions.onlyIf previously always
// serialized `{}` even when unset. A handle-type reference field is
// already *Name via declRefConv and is unaffected; likewise a "types:"
// override naming anything other than an included data declaration
// (js.Value, int, []string, ...) is left exactly as specified.
//
// It also sets curNameHint (structName+fieldName, per item 1's naming
// rule) around type resolution, and folds any notef notes raised while
// resolving m's type (e.g. a union resolved via the single-included-ref
// rule) into the doc string it returns — the caller uses that in place of
// m.Doc for the emitted comment.
func (p *Package) fieldConv(d *ir.Decl, m ir.Member) (exprConv, string, error) {
	override := p.typeOverride(d.Name, m.Name, "")
	structName := p.declGoName(d)
	fieldName := p.memberName(d.Name, m.Name)
	prevMember := p.curMemberName
	p.curMemberName = m.Name
	p.resetNotes()
	conv, err := p.convForNamed(m.Type, override, structName+fieldName)
	notes := p.takeNotes()
	p.curMemberName = prevMember
	if err != nil {
		return exprConv{}, "", err
	}
	if m.Optional || isNullableType(m.Type) {
		if isDataStructConv(conv) {
			conv = pointerWrap(conv)
		}
	}
	return conv, appendNotesToDoc(m.Doc, notes), nil
}

// unionConv resolves a union type per tmp/06-codegen-spec.md 5.1 item 2's
// default rules (tried in order below), falling back to the pre-item-2
// behavior — js.Value with a warning — only when none of them apply:
//
//  1. every non-null/undefined member is a string literal or plain string
//     -> string
//  2. exactly one member remains after stripping null/undefined -> that
//     member's own conversion (unchanged from before item 2)
//  3. every remaining member is a numeric literal (or plain number) ->
//     float64
//  4. every remaining member is boolean-ish (a bool, a bool literal, or a
//     union of only those) -> bool
//  5. every remaining member resolves to a data shape (an object literal,
//     or a ref to one - including one outside this package's include
//     list, matching the existing top-level-alias union-merge rule) ->
//     a synthesized merged data type (item 1)
//  6. every remaining member is a ref, and exactly one of them names a
//     declaration in this package's include list -> that declaration,
//     with an info-level note (not a warning) on the affected field/
//     method's doc comment recording which branch was chosen
func (p *Package) unionConv(t *ir.Type) (exprConv, error) {
	var nonNull []ir.Type
	for _, mt := range t.Types {
		if mt.K == "prim" && (mt.Name == "null" || mt.Name == "undefined") {
			continue
		}
		nonNull = append(nonNull, mt)
	}
	if len(nonNull) == 0 {
		return jsValueConv(), nil
	}
	if allStringy(nonNull) {
		return scalarConv("string", ".String()", `""`), nil
	}
	if len(nonNull) == 1 {
		return p.convFor(&nonNull[0], "")
	}
	if allNumbery(nonNull) {
		return scalarConv("float64", ".Float()", "0"), nil
	}
	if allBooleanishTypes(nonNull) {
		return boolConv(), nil
	}
	if members, ok := resolveUnionMembers(p.declByName, nonNull, 0); ok {
		return p.synthesizeDataType(members)
	}
	if conv, ok, err := p.singleIncludedRefUnion(nonNull); err != nil {
		return exprConv{}, err
	} else if ok {
		return conv, nil
	}
	p.warnCtxf("unsupported union type, falling back to js.Value")
	return jsValueConv(), nil
}

// allStringy reports whether every type in types is either the "string"
// primitive or a string literal (tmp/06-codegen-spec.md 5.1 item 2's
// "文字列リテラルと string の混在 → string").
func allStringy(types []ir.Type) bool {
	if len(types) == 0 {
		return false
	}
	for i := range types {
		t := &types[i]
		if t.K == "prim" && t.Name == "string" {
			continue
		}
		if t.IsStringLiteral() {
			continue
		}
		return false
	}
	return true
}

// allNumbery reports whether every type in types is either the "number"
// primitive or a numeric literal (item 2's "数値リテラルの union → float64",
// extended defensively to a union mixing in the plain type too, the same
// way allStringy handles string).
func allNumbery(types []ir.Type) bool {
	if len(types) == 0 {
		return false
	}
	for i := range types {
		t := &types[i]
		if t.K == "prim" && t.Name == "number" {
			continue
		}
		if t.K == "literal" {
			if _, ok := t.Value.(float64); ok {
				continue
			}
		}
		return false
	}
	return true
}

// allBooleanishTypes reports whether every type in types is boolean-ish, per
// isBooleanish (flatten.go) — item 2's "boolean と真偽リテラルの混在 →
// bool".
func allBooleanishTypes(types []ir.Type) bool {
	if len(types) == 0 {
		return false
	}
	for i := range types {
		if !isBooleanish(&types[i]) {
			return false
		}
	}
	return true
}

// singleIncludedRefUnion implements item 2's ref-union rule: if every type
// in nonNull is a plain ref, and exactly one of them names a declaration in
// this package's include list, that declaration's conversion is used (ok
// is true), with an info-level note recording the choice. Otherwise ok is
// false (including when nonNull contains a non-ref type at all, in which
// case the caller's ordinary warning applies instead).
func (p *Package) singleIncludedRefUnion(nonNull []ir.Type) (exprConv, bool, error) {
	for i := range nonNull {
		if nonNull[i].K != "ref" {
			return exprConv{}, false, nil
		}
	}
	var chosen *ir.Type
	var names []string
	count := 0
	for i := range nonNull {
		names = append(names, nonNull[i].Name)
		if _, ok := p.included[nonNull[i].Name]; ok {
			count++
			chosen = &nonNull[i]
		}
	}
	if count != 1 {
		return exprConv{}, false, nil
	}
	conv, err := p.convFor(chosen, "")
	if err != nil {
		return exprConv{}, false, err
	}
	p.notef("resolved union (%s) to %s, the only member declared in this package's include list.", strings.Join(names, " | "), chosen.Name)
	return conv, true, nil
}

// convForOverride resolves a "types:" Go type override string. Besides the
// fixed set of built-in spellings, per tmp/06-codegen-spec.md 2.1 item 6 the
// override may also name a declaration in this package's include list
// (used to pick one branch of an otherwise-unresolvable union, e.g.
// "R2HTTPMetadata" for a field typed `R2HTTPMetadata | Headers`), optionally
// wrapped as a slice ("[]MessageSendRequest").
func (p *Package) convForOverride(t *ir.Type, override string) (exprConv, error) {
	if elemName, ok := strings.CutPrefix(override, "[]"); ok {
		elemConv, err := p.namedTypeConv(elemName)
		if err != nil {
			return exprConv{}, fmt.Errorf("unsupported types: override %q: %w", override, err)
		}
		return arrayOfConv(elemConv, p.curContainerDepth), nil
	}
	return p.namedTypeConv(override)
}

// namedTypeConv resolves a single (non-slice) "types:" override spelling:
// either one of the fixed built-in names, or the name of a declaration in
// this package's include list.
func (p *Package) namedTypeConv(override string) (exprConv, error) {
	switch override {
	case "js.Value":
		return jsValueConv(), nil
	case "io.Reader":
		p.useImport("jsrt")
		p.useImport("io")
		return readerParamConv(), nil
	case "http.Header":
		// tmp/06-codegen-spec.md 5.1 item 4: lets a "types:" override pick
		// the Headers side of a union cfgen can't otherwise resolve on
		// its own (e.g. images' HeadersInit, a "Headers |
		// Record<string,string> | [string,string][]" union), the same
		// way "R2HTTPMetadata" picks a data-type branch.
		p.useImport("jsrt")
		p.useImport("net/http")
		return headersConv(), nil
	case "time.Time":
		// Used to pick the Date side of a "number | Date" union a param
		// can't otherwise resolve (e.g. DurableObjectStorage.setAlarm's
		// scheduledTime), per tmp/06-codegen-spec.md 4.1.
		p.useImport("jsrt")
		p.useImport("time")
		return dateConv(), nil
	case "string":
		return scalarConv("string", ".String()", `""`), nil
	case "float32":
		return scalarConv32("float32", ".Float()"), nil
	case "float64":
		return scalarConv("float64", ".Float()", "0"), nil
	case "bool":
		return boolConv(), nil
	case "int":
		return exprConv{
			GoType:     "int",
			FromJS:     func(dst, src, _ string) []string { return []string{dst + " = " + src + ".Int()"} },
			ToJS:       func(src string) ([]string, string) { return nil, src },
			ZeroExpr:   "0",
			OmitIfZero: func(expr string) string { return expr + " != 0" },
		}, nil
	case "*int":
		return exprConv{
			GoType: "*int",
			FromJS: func(dst, src, _ string) []string {
				return []string{"n := " + src + ".Int()", dst + " = &n"}
			},
			ToJS:       func(src string) ([]string, string) { return nil, "*" + src },
			ZeroExpr:   "nil",
			OmitIfZero: func(expr string) string { return expr + " != nil" },
		}, nil
	case "map[string]any":
		p.useImport("jsrt")
		return exprConv{
			GoType: "map[string]any",
			FromJS: func(dst, src, _ string) []string {
				return []string{
					dst + " = make(map[string]any)",
					"keys := js.Global().Get(\"Object\").Call(\"keys\", " + src + ")",
					"for i := 0; i < keys.Length(); i++ {",
					"\tk := keys.Index(i).String()",
					"\t" + dst + "[k] = " + src + ".Get(k)",
					"}",
				}
			},
			ToJS: func(src string) ([]string, string) {
				return []string{
					"m := jsrt.NewObject()",
					"for k, v := range " + src + " {",
					"\tm.Set(k, v)",
					"}",
				}, "m"
			},
			ZeroExpr:   "nil",
			OmitIfZero: func(expr string) string { return "len(" + expr + ") > 0" },
		}, nil
	default:
		if d, ok := p.included[override]; ok {
			return p.declRefConv(d)
		}
		return exprConv{}, fmt.Errorf("unsupported types: override %q", override)
	}
}

func scalarConv32(goType, accessor string) exprConv {
	return exprConv{
		GoType:     goType,
		FromJS:     func(dst, src, _ string) []string { return []string{dst + " = " + goType + "(" + src + accessor + ")"} },
		ToJS:       func(src string) ([]string, string) { return nil, src },
		ZeroExpr:   "0",
		OmitIfZero: func(expr string) string { return expr + " != 0" },
	}
}

func indentAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if l == "" {
			out[i] = ""
		} else {
			out[i] = "\t" + l
		}
	}
	return out
}
