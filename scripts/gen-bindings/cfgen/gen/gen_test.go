package gen

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syumai/workers-go/scripts/gen-bindings/cfgen/ir"
)

var update = flag.Bool("update", false, "update golden files")

func loadFixtureIR(t *testing.T, path string) *ir.IR {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc ir.IR
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return &doc
}

// TestGenerateGolden exercises the full generation pipeline (classification,
// type mapping, naming, and formatting) against a small hand-written IR
// fixture covering: a handle type, a data type, a string-literal alias
// enum, a Promise-returning method, a Record-returning method, an array
// field, an optional field, a rename override, a types override, and a
// handwritten method.
func TestGenerateGolden(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture.json"))
	ov, err := LoadOverrides(filepath.Join("testdata", "fixture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ov.Validate(doc); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(doc, ov)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	goldenPath := filepath.Join("testdata", "fixture.golden.go.txt")
	if *update {
		if err := os.WriteFile(goldenPath, result.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Source) != string(want) {
		t.Errorf("generated output does not match golden file %s (run `go test ./cfgen/gen/... -update` to refresh it if the change is intentional)\n--- got ---\n%s\n--- want ---\n%s", goldenPath, result.Source, want)
	}
}

// TestValidateRejectsUnknownNames ensures cfgen fails loudly (rather than
// silently ignoring) when an overrides file references a declaration or
// member that does not exist, per spec 1.3 "YAML に存在しない宣言名・
// メンバー名を書いたら cfgen はエラーで止まる".
func TestValidateRejectsUnknownNames(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture.json"))

	cases := []struct {
		name string
		ov   Overrides
	}{
		{"unknown include", Overrides{Package: "x", Include: []string{"DoesNotExist"}}},
		{"unknown rename target", Overrides{Package: "x", Include: []string{"Widget"}, Rename: map[string]string{"Widget.nope": "Nope"}}},
		{"unknown types target", Overrides{Package: "x", Include: []string{"WidgetInfo"}, Types: map[string]string{"WidgetInfo.nope": "string"}}},
		{"unknown handwritten target", Overrides{Package: "x", Include: []string{"Widget"}, Handwritten: []string{"Widget.nope"}}},
		{"unknown exclude target", Overrides{Package: "x", Include: []string{"Widget"}, Exclude: []string{"Widget.nope"}}},
		{"binding not in include", Overrides{Package: "x", Include: []string{"WidgetInfo"}, Bindings: []string{"Widget"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.ov.Path = "fixture"
			if err := c.ov.Validate(doc); err == nil {
				t.Errorf("expected an error, got nil")
			}
		})
	}
}

// TestIntersectionFallsBackWhenUnresolvable verifies that GeoUnresolvable
// (an alias intersecting a resolvable ref with one that isn't in the IR at
// all) falls back to the pre-flattening behavior — an opaque js.Value type
// alias, with a warning — rather than silently merging only the fields it
// could resolve (which would misrepresent the shape).
func TestIntersectionFallsBackWhenUnresolvable(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture.json"))
	ov := &Overrides{Package: "x", Include: []string{"GeoUnresolvable"}, Path: "fixture"}
	if err := ov.Validate(doc); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(doc, ov)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Errorf("expected a fallback warning, got none")
	}
	if !strings.Contains(string(result.Source), "type GeoUnresolvable = js.Value") {
		t.Errorf("expected GeoUnresolvable to fall back to js.Value, got:\n%s", result.Source)
	}
}

// TestDeclHasMemberSeesFlattenedFields verifies that an override key can
// reference a field GeoExt only has by virtue of extends-flattening
// (lat, inherited-then-overridden from GeoBase; lng is GeoExt's own), not
// just its own direct members.
func TestDeclHasMemberSeesFlattenedFields(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture.json"))
	ov := &Overrides{
		Package: "x",
		Include: []string{"GeoExt"},
		Rename:  map[string]string{"GeoExt.lat": "Latitude"},
		Path:    "fixture",
	}
	if err := ov.Validate(doc); err != nil {
		t.Fatalf("Validate() failed for a rename targeting a flattened field: %v", err)
	}
}

// TestGenerateGolden2 exercises the cfgen extensions from
// tmp/06-codegen-spec.md section 2.1: overload index/literal splitting with
// a skipped variant (and its warning), typeParam defaults, nullable
// Promise returns for a prim/handle/data type, handle-extends flattening,
// union-of-object-literals data merging (as a top-level alias and as an
// intersection operand), and Iterable<T> params.
func TestGenerateGolden2(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture2.json"))
	ov, err := LoadOverrides(filepath.Join("testdata", "fixture2.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ov.Validate(doc); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(doc, ov)
	if err != nil {
		t.Fatal(err)
	}
	wantWarnings := []string{
		// Encoding (Store.get's overload-2 method typeParam) has no
		// default, so it falls back to js.Value with a warning, same as
		// any other unresolvable typeParam (spec item 2's "no default"
		// case).
		`type parameter "Encoding" used outside of a supported context, falling back to js.Value`,
		`Store.get: skipping overload 0 (2 params) with no overloads: entry`,
	}
	if !slicesEqual(wantWarnings, result.Warnings) {
		t.Errorf("Warnings = %v, want %v", result.Warnings, wantWarnings)
	}

	// tmp/06-codegen-spec.md 1.3's nested-data-type pointer rule: an
	// optional or nullable field whose type is itself an included data
	// type becomes *T (Config.Opt, Config.Nullable), while a required one
	// of the same type stays a plain value (Config.Req), and an optional
	// handle-type ref is unaffected (Config.Owner was already *Parent).
	src := string(result.Source)
	for _, want := range []string{
		"Req      Box     `js:\"req\"`",
		"Opt      *Box    `js:\"opt\"`",
		"Nullable *Box    `js:\"nullable\"`",
		"Owner    *Parent `js:\"owner\"`",
		"if !jsrt.IsNil(v.Get(\"opt\")) {",
		"if o.Opt != nil {",
		"obj.Set(\"opt\", (*o.Opt).toJS())",
		// A types: override naming an included data declaration
		// (Config.altBox -> Box) triggers the same pointer treatment.
		"AltBox *Box `js:\"altBox\"`",
		"if o.AltBox != nil {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source missing %q", want)
		}
	}

	goldenPath := filepath.Join("testdata", "fixture2.golden.go.txt")
	if *update {
		if err := os.WriteFile(goldenPath, result.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Source) != string(want) {
		t.Errorf("generated output does not match golden file %s (run `go test ./cfgen/gen/... -update` to refresh it if the change is intentional)\n--- got ---\n%s\n--- want ---\n%s", goldenPath, result.Source, want)
	}
}

// TestOverloadsRejectsBadEntries covers the overloads: validation errors:
// an out-of-range index, a duplicate index, and a literal that doesn't
// match any parameter of the selected overload (an upstream reordering
// guard, per spec item 1).
func TestOverloadsRejectsBadEntries(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture2.json"))
	cases := []struct {
		name string
		ov   Overrides
	}{
		{
			"index out of range",
			Overrides{Package: "x", Include: []string{"Store", "Parent", "Box"}, Overloads: map[string][]OverloadEntry{
				"Store.get": {{Index: 99, Name: "GetText", Literal: "text"}},
			}},
		},
		{
			"duplicate index",
			Overrides{Package: "x", Include: []string{"Store", "Parent", "Box"}, Overloads: map[string][]OverloadEntry{
				"Store.get": {{Index: 1, Name: "GetText", Literal: "text"}, {Index: 1, Name: "GetText2", Literal: "text"}},
			}},
		},
		{
			"literal mismatch",
			Overrides{Package: "x", Include: []string{"Store", "Parent", "Box"}, Overloads: map[string][]OverloadEntry{
				"Store.get": {{Index: 1, Name: "GetText", Literal: "does-not-exist"}},
			}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.ov.Path = "fixture2"
			if err := c.ov.Validate(doc); err != nil {
				// Index-range/duplicate checks happen in Validate.
				return
			}
			if _, err := Generate(doc, &c.ov); err == nil {
				t.Errorf("expected an error, got nil")
			}
		})
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPascalCase(t *testing.T) {
	cases := map[string]string{
		"cacheTtl":       "CacheTTL",
		"asOrganization": "AsOrganization",
		"md5":            "MD5",
		"id":             "ID",
		"widget_name":    "WidgetName",
		"success":        "Success",
		"httpStatus":     "HTTPStatus",
	}
	for in, want := range cases {
		if got := pascalCase(in); got != want {
			t.Errorf("pascalCase(%q) = %q, want %q", in, got, want)
		}
	}
}
