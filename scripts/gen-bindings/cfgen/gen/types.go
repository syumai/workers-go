package gen

import (
	"fmt"
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
}

func NewPackage(doc *ir.IR, ov *Overrides) *Package {
	p := &Package{
		IR:         doc,
		Ov:         ov,
		declByName: indexDecls(doc),
		included:   map[string]*ir.Decl{},
		imports:    map[string]bool{},
	}
	for _, name := range ov.Include {
		if d, ok := p.declByName[name]; ok {
			p.included[name] = d
		}
	}
	return p
}

func (p *Package) warnf(format string, args ...any) {
	p.warnings = append(p.warnings, fmt.Sprintf(format, args...))
}

// Warnings returns the warnings collected during generation.
func (p *Package) Warnings() []string { return p.warnings }

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
		p.warnf("intersection types are not supported, falling back to js.Value")
		return jsValueConv(), nil
	case "object":
		p.warnf("inline object type literals in field position are not supported, falling back to js.Value")
		return jsValueConv(), nil
	case "function":
		p.warnf("function types are not supported, falling back to js.Value")
		return jsValueConv(), nil
	case "typeParam":
		if def := p.typeParamDefault(t.Name); def != nil {
			return p.convFor(def, "")
		}
		p.warnf("type parameter %q used outside of a supported context, falling back to js.Value", t.Name)
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
	ec, err := p.convFor(elem, "")
	if err != nil {
		return exprConv{}, err
	}
	return arrayOfConv(ec), nil
}

// arrayOfConv builds a plain-JS-Array <-> Go slice conversion given the
// element conversion. Element conversions that themselves require
// pre-statements in ToJS are not supported (not needed by the current
// generation targets) and fall back with a warning handled by the caller.
func arrayOfConv(elem exprConv) exprConv {
	goType := "[]" + elem.GoType
	return exprConv{
		GoType: goType,
		FromJS: func(dst, src, failReturn string) []string {
			lines := []string{
				dst + " = make(" + goType + ", " + src + ".Length())",
				"for i := range " + dst + " {",
			}
			inner := elem.FromJS(dst+"[i]", src+".Index(i)", failReturn)
			lines = append(lines, indentAll(inner)...)
			lines = append(lines, "}")
			return lines
		},
		ToJS: func(src string) ([]string, string) {
			arrVar := "arr"
			pre, elemExpr := elem.ToJS("e")
			var lines []string
			lines = append(lines, arrVar+" := js.Global().Get(\"Array\").New(len("+src+"))")
			if len(pre) == 0 {
				lines = append(lines, "for i, e := range "+src+" {")
				lines = append(lines, "\t"+arrVar+".SetIndex(i, "+elemExpr+")")
				lines = append(lines, "}")
			} else {
				lines = append(lines, "for i, e := range "+src+" {")
				lines = append(lines, indentAll(pre)...)
				lines = append(lines, "\t"+arrVar+".SetIndex(i, "+elemExpr+")")
				lines = append(lines, "}")
			}
			return lines, arrVar
		},
		ZeroExpr:   "nil",
		OmitIfZero: func(expr string) string { return "len(" + expr + ") > 0" },
	}
}

func (p *Package) recordConv(val *ir.Type) (exprConv, error) {
	vc, err := p.convFor(val, "")
	if err != nil {
		return exprConv{}, err
	}
	goType := "map[string]" + vc.GoType
	return exprConv{
		GoType: goType,
		FromJS: func(dst, src, failReturn string) []string {
			lines := []string{
				dst + " = make(" + goType + ")",
				"keys := js.Global().Get(\"Object\").Call(\"keys\", " + src + ")",
				"for i := 0; i < keys.Length(); i++ {",
				"\tk := keys.Index(i).String()",
			}
			var vv string
			lines = append(lines, "\tvar vv "+vc.GoType)
			vv = "vv"
			inner := vc.FromJS(vv, src+".Get(k)", failReturn)
			lines = append(lines, indentAll(inner)...)
			lines = append(lines, "\t"+dst+"[k] = "+vv)
			lines = append(lines, "}")
			return lines
		},
		ToJS: func(src string) ([]string, string) {
			pre, valExpr := vc.ToJS("v")
			lines := []string{
				"m := jsrt.NewObject()",
				"for k, v := range " + src + " {",
			}
			lines = append(lines, indentAll(pre)...)
			lines = append(lines, "\tm.Set(k, "+valExpr+")")
			lines = append(lines, "}")
			return lines, "m"
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
		return readCloserConv(), nil
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
			ToJS:       func(src string) ([]string, string) { return nil, src + ".toJS()" },
			ZeroExpr:   name + "{}",
			OmitIfZero: func(string) string { return "true" },
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

// nestedDataDeclFor returns the included data-shaped declaration a struct
// field resolves to — either straight from its IR type (after stripping a
// null/undefined union variant), or, when override is non-empty, from a
// "types:" override that names an included declaration (e.g.
// "R2Conditional", used to pick one branch of an otherwise-unresolvable
// union) — or nil if it doesn't refer to one at all: a handle ref (already
// *Name, correctly omitted via its own OmitIfZero), a scalar, js.Value, a
// slice/map, or an override naming something other than an included data
// declaration ("js.Value", "int", "[]string", ...).
func (p *Package) nestedDataDeclFor(fieldType *ir.Type, override string) *ir.Decl {
	name := override
	if name == "" {
		target := fieldType
		if nn, ok := splitNullable(fieldType); ok {
			target = nn
		}
		if target == nil || target.K != "ref" {
			return nil
		}
		name = target.Name
	}
	d, ok := p.included[name]
	if !ok {
		return nil
	}
	switch classify(p.declByName, d) {
	case KindData, KindAliasData:
		return d
	default:
		return nil
	}
}

// fieldConv resolves the conversion for one data-type struct field
// (property member m of data-shaped declaration d), applying a "types:"
// override if present and, per tmp/06-codegen-spec.md 1.3's "data 型" rule,
// pointer-wrapping an optional-or-nullable field whose type resolves to a
// nested data-type reference — whether directly (`field?: Other` /
// `field: Other | null` / `field: Other | undefined`) or via a "types:"
// override naming an included data declaration (e.g. R2GetOptions.onlyIf's
// `types: R2Conditional`, picking one branch of an `R2Conditional |
// Headers` union) — into `*Other`, omitted entirely when nil in toJS and
// only allocated-and-decoded when present in fromJS. Without this, a
// plain (non-pointer) struct field can't tell its zero value apart from
// "the caller didn't set this", so toJS always sent it — e.g.
// R2GetOptions.range / R2PutOptions.onlyIf previously always serialized
// `{}` even when unset. A handle-type reference field is already *Name via
// declRefConv and is unaffected; likewise a "types:" override naming
// anything other than an included data declaration (js.Value, int,
// []string, ...) is left exactly as specified.
func (p *Package) fieldConv(d *ir.Decl, m ir.Member) (exprConv, error) {
	override := p.typeOverride(d.Name, m.Name, "")
	conv, err := p.convFor(m.Type, override)
	if err != nil {
		return exprConv{}, err
	}
	if m.Optional || isNullableType(m.Type) {
		if p.nestedDataDeclFor(m.Type, override) != nil {
			conv = pointerWrap(conv)
		}
	}
	return conv, nil
}

func (p *Package) unionConv(t *ir.Type) (exprConv, error) {
	if allStringLiterals(t.Types) {
		return scalarConv("string", ".String()", `""`), nil
	}
	var nonNull []ir.Type
	for _, mt := range t.Types {
		if mt.K == "prim" && (mt.Name == "null" || mt.Name == "undefined") {
			continue
		}
		nonNull = append(nonNull, mt)
	}
	if len(nonNull) == 1 {
		return p.convFor(&nonNull[0], "")
	}
	p.warnf("unsupported union type, falling back to js.Value")
	return jsValueConv(), nil
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
		return arrayOfConv(elemConv), nil
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
