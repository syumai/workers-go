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

// classify determines how a declaration should be generated.
func classify(d *ir.Decl) DeclKind {
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
		p.warnf("type parameter %q used outside of a supported context, falling back to js.Value", t.Name)
		return jsValueConv(), nil
	case "unsupported":
		p.warnf("unsupported TypeScript construct %q, falling back to js.Value", t.Text)
		return jsValueConv(), nil
	}
	p.warnf("unrecognized IR type kind %q, falling back to js.Value", t.K)
	return jsValueConv(), nil
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
	case "Array":
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
	switch classify(d) {
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
	default: // KindAliasType: recurse into the underlying type.
		return p.convFor(d.Type, "")
	}
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

// convForOverride resolves a "types:" Go type override string.
func (p *Package) convForOverride(t *ir.Type, override string) (exprConv, error) {
	switch override {
	case "js.Value":
		return jsValueConv(), nil
	case "[]string":
		return arrayOfConv(scalarConv("string", ".String()", `""`)), nil
	case "[]float32":
		return arrayOfConv(scalarConv32("float32", ".Float()")), nil
	case "[]float64":
		return arrayOfConv(scalarConv("float64", ".Float()", "0")), nil
	case "[]bool":
		return arrayOfConv(boolConv()), nil
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
