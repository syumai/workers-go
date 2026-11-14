//go:build js && wasm

package durableobjects

import (
	"syscall/js"
	"testing"
	"time"
)

func resolvedPromise(v any) js.Value {
	return js.Global().Get("Promise").Call("resolve", v)
}

// TestDurableObjectStorage_Get_Missing exercises the Get/GetString round
// trip when the underlying JS storage.get resolves to undefined (no such
// key): Get itself should report that with IsNil, and GetString should
// translate it into its own "not found" bool rather than the string
// "undefined".
func TestDurableObjectStorage_Get_Missing(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	var gotKey string
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotKey = args[0].String()
		return resolvedPromise(js.Undefined())
	}))

	s := DurableObjectStorageFromJS(fake)
	v, err := s.Get("missing", DurableObjectGetOptions{})
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if !v.IsUndefined() {
		t.Fatalf("Get() = %v, want undefined", v)
	}
	if gotKey != "missing" {
		t.Fatalf("key sent to JS = %q, want %q", gotKey, "missing")
	}

	str, ok, err := s.GetString("missing")
	if err != nil {
		t.Fatalf("GetString() failed: %v", err)
	}
	if ok {
		t.Fatalf("GetString() ok = true, want false")
	}
	if str != "" {
		t.Fatalf("GetString() = %q, want empty string", str)
	}
}

// TestDurableObjectStorage_Put exercises Put and PutString: the JS side's
// storage.put(key, value, options) receives the key and the exact value
// (unwrapped from the js.Value/string argument), and the returned Promise's
// resolution surfaces as a nil error.
func TestDurableObjectStorage_Put(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	var gotKey, gotValue string
	fake.Set("put", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotKey = args[0].String()
		gotValue = args[1].String()
		return resolvedPromise(js.Undefined())
	}))

	s := DurableObjectStorageFromJS(fake)
	if err := s.PutString("greeting", "hello"); err != nil {
		t.Fatalf("PutString() failed: %v", err)
	}
	if gotKey != "greeting" || gotValue != "hello" {
		t.Fatalf("put(%q, %q), want put(%q, %q)", gotKey, gotValue, "greeting", "hello")
	}
}

// TestDurableObjectStorage_List exercises the Map<string, T> decoding added
// for tmp/06-codegen-spec.md 4.1: the fake storage.list resolves with a real
// JS Map (as the actual runtime would), and List must decode it into a Go
// map[string]js.Value via Array.from(keys())/get(k), not Object.keys (which
// doesn't see a Map's entries).
func TestDurableObjectStorage_List(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("list", js.FuncOf(func(this js.Value, args []js.Value) any {
		m := js.Global().Get("Map").New()
		m.Call("set", "a", "1")
		m.Call("set", "b", "2")
		return resolvedPromise(m)
	}))

	s := DurableObjectStorageFromJS(fake)
	got, err := s.List(DurableObjectListOptions{})
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List() returned %d entries, want 2 (%v)", len(got), got)
	}
	if v, ok := got["a"]; !ok || v.String() != "1" {
		t.Errorf(`List()["a"] = %v, %v, want "1", true`, v, ok)
	}
	if v, ok := got["b"]; !ok || v.String() != "2" {
		t.Errorf(`List()["b"] = %v, %v, want "2", true`, v, ok)
	}
}

// TestDurableObjectStorage_SetAlarm verifies that the scheduledTime
// parameter — a "number | Date" union in the .d.ts, forced to time.Time via
// a types: override per tmp/06-codegen-spec.md 4.1 — is actually sent to JS
// as a Date instance (the "Date side" of the union), not a plain number.
func TestDurableObjectStorage_SetAlarm(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	var gotIsDate bool
	var gotMs float64
	fake.Set("setAlarm", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotIsDate = args[0].InstanceOf(js.Global().Get("Date"))
		gotMs = args[0].Call("getTime").Float()
		return resolvedPromise(js.Undefined())
	}))

	s := DurableObjectStorageFromJS(fake)
	when := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := s.SetAlarm(when, DurableObjectSetAlarmOptions{}); err != nil {
		t.Fatalf("SetAlarm() failed: %v", err)
	}
	if !gotIsDate {
		t.Fatalf("setAlarm's scheduledTime arg is not a JS Date instance")
	}
	if want := float64(when.UnixMilli()); gotMs != want {
		t.Fatalf("setAlarm's scheduledTime = %v ms, want %v ms", gotMs, want)
	}
}

// TestDurableObjectStorage_Transaction exercises tmp/06-codegen-spec.md 5.1
// item 5's callback-method support: Transaction's Go closure parameter is
// wrapped as a JS function via jsrt.AsyncFunc, and the fake "transaction"
// JS method below invokes it with a fake DurableObjectTransaction-shaped
// object whose closure calls txn.Put — verifying the closure not only runs
// but that the *DurableObjectTransaction it receives is a fully usable
// generated handle (able to make its own further JS calls), not just an
// opaque js.Value.
func TestDurableObjectStorage_Transaction(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	var gotKey, gotValue string
	fake.Set("transaction", js.FuncOf(func(this js.Value, args []js.Value) any {
		closure := args[0]
		txn := js.ValueOf(map[string]any{})
		txn.Set("put", js.FuncOf(func(this js.Value, args []js.Value) any {
			gotKey = args[0].String()
			gotValue = args[1].String()
			return resolvedPromise(js.Undefined())
		}))
		// closure(txn) returns a Promise (jsrt.AsyncFunc's own wrapping);
		// transaction itself resolves once that settles, with the
		// closure's own resolved value.
		return closure.Invoke(txn)
	}))

	s := DurableObjectStorageFromJS(fake)
	result, err := s.Transaction(func(txn *DurableObjectTransaction) (js.Value, error) {
		if err := txn.Put("k1", js.ValueOf("v1"), DurableObjectPutOptions{}); err != nil {
			return js.Value{}, err
		}
		return js.ValueOf("committed"), nil
	})
	if err != nil {
		t.Fatalf("Transaction() failed: %v", err)
	}
	if gotKey != "k1" || gotValue != "v1" {
		t.Fatalf("txn.Put() received (%q, %q), want (%q, %q)", gotKey, gotValue, "k1", "v1")
	}
	if result.String() != "committed" {
		t.Fatalf("Transaction() result = %q, want %q", result.String(), "committed")
	}
}

// TestDurableObjectState_BlockConcurrencyWhile exercises the
// zero-argument-callback shape of 5.1 item 5's callback-method support
// ("() => Promise<U>", as opposed to Transaction's "(a: A) => Promise<U>"):
// the fake "blockConcurrencyWhile" JS method invokes the wrapped Go closure
// with no arguments and returns its resolved value.
func TestDurableObjectState_BlockConcurrencyWhile(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	var called bool
	fake.Set("blockConcurrencyWhile", js.FuncOf(func(this js.Value, args []js.Value) any {
		called = true
		return args[0].Invoke()
	}))

	s := DurableObjectStateFromJS(fake)
	result, err := s.BlockConcurrencyWhile(func() (js.Value, error) {
		return js.ValueOf(float64(42)), nil
	})
	if err != nil {
		t.Fatalf("BlockConcurrencyWhile() failed: %v", err)
	}
	if !called {
		t.Fatalf("the fake blockConcurrencyWhile was never invoked")
	}
	if result.Float() != 42 {
		t.Fatalf("BlockConcurrencyWhile() result = %v, want 42", result.Float())
	}
}

// TestDurableObjectStorage_GetJSON_PutJSON round-trips a Go struct through
// PutJSON/GetJSON against a fake storage backed by a plain Go map, verifying
// the JSON.stringify/JSON.parse bridge documented on both methods.
func TestDurableObjectStorage_GetJSON_PutJSON(t *testing.T) {
	type record struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	store := map[string]js.Value{}
	fake := js.ValueOf(map[string]any{})
	fake.Set("put", js.FuncOf(func(this js.Value, args []js.Value) any {
		store[args[0].String()] = args[1]
		return resolvedPromise(js.Undefined())
	}))
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		v, ok := store[args[0].String()]
		if !ok {
			return resolvedPromise(js.Undefined())
		}
		return resolvedPromise(v)
	}))

	s := DurableObjectStorageFromJS(fake)
	want := record{Name: "Ada", Age: 36}
	if err := s.PutJSON("person", want); err != nil {
		t.Fatalf("PutJSON() failed: %v", err)
	}

	var got record
	ok, err := s.GetJSON("person", &got)
	if err != nil {
		t.Fatalf("GetJSON() failed: %v", err)
	}
	if !ok {
		t.Fatalf("GetJSON() ok = false, want true")
	}
	if got != want {
		t.Fatalf("GetJSON() = %+v, want %+v", got, want)
	}

	var missing record
	if ok, err := s.GetJSON("does-not-exist", &missing); err != nil || ok {
		t.Fatalf("GetJSON(\"does-not-exist\") = %v, %v, want false, nil", ok, err)
	}
}

// TestSQLStorageCursor_Rows exercises the storage.go Rows helper against a
// fake cursor whose toArray()/columnNames mimic SqlStorage.exec's real
// shape: an array of plain row objects keyed by column name, with values
// covering every SqlStorageValue case (string, number, null, ArrayBuffer).
func TestSQLStorageCursor_Rows(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("columnNames", js.ValueOf([]any{"name", "count", "note", "blob"}))
	row := js.ValueOf(map[string]any{})
	row.Set("name", "widget")
	row.Set("count", 3)
	row.Set("note", nil)
	buf := js.Global().Get("Uint8Array").New(3)
	js.CopyBytesToJS(buf, []byte{1, 2, 3})
	row.Set("blob", buf.Get("buffer"))
	fake.Set("toArray", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.Global().Get("Array").New(1).Call("fill", row)
	}))

	c := SQLStorageCursorFromJS(fake)
	rows, err := c.Rows()
	if err != nil {
		t.Fatalf("Rows() failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Rows() returned %d rows, want 1", len(rows))
	}
	got := rows[0]
	if got["name"] != "widget" {
		t.Errorf(`Rows()[0]["name"] = %v, want "widget"`, got["name"])
	}
	if got["count"] != float64(3) {
		t.Errorf(`Rows()[0]["count"] = %v, want 3`, got["count"])
	}
	if got["note"] != nil {
		t.Errorf(`Rows()[0]["note"] = %v, want nil`, got["note"])
	}
	blob, ok := got["blob"].([]byte)
	if !ok {
		t.Fatalf(`Rows()[0]["blob"] has type %T, want []byte`, got["blob"])
	}
	if string(blob) != "\x01\x02\x03" {
		t.Errorf(`Rows()[0]["blob"] = %v, want [1 2 3]`, blob)
	}
}

// TestSQLStorage_Exec_BytesBinding is a regression test for generated
// "...any" rest parameters: SqlStorage.exec's bindings must reach JS with a
// []byte as a Uint8Array (and a time.Time as a Date) rather than panicking in
// js.ValueOf, which is what the generated Exec did before cfgen routed rest
// elements through jsrt.AnyArgs.
func TestSQLStorage_Exec_BytesBinding(t *testing.T) {
	var gotQuery string
	var gotBlob []byte
	var blobIsU8 bool
	var gotNum int
	fake := js.ValueOf(map[string]any{})
	fake.Set("exec", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotQuery = args[0].String()
		blob := args[1]
		blobIsU8 = blob.InstanceOf(js.Global().Get("Uint8Array"))
		gotBlob = make([]byte, blob.Length())
		js.CopyBytesToGo(gotBlob, blob)
		gotNum = args[2].Int()
		return js.ValueOf(map[string]any{})
	}))

	s := SQLStorageFromJS(fake)
	if _, err := s.Exec("INSERT INTO t VALUES (?, ?)", []byte{1, 2, 3}, 42); err != nil {
		t.Fatalf("Exec() with []byte binding failed: %v", err)
	}
	if gotQuery != "INSERT INTO t VALUES (?, ?)" {
		t.Errorf("query = %q", gotQuery)
	}
	if !blobIsU8 || string(gotBlob) != "\x01\x02\x03" {
		t.Errorf("blob binding = %v (Uint8Array=%v), want Uint8Array [1 2 3]", gotBlob, blobIsU8)
	}
	if gotNum != 42 {
		t.Errorf("number binding = %d, want 42", gotNum)
	}
}

// TestSQLStorage_Exec_RejectsSlicesAndMaps checks that bindings with no SQL
// representation are rejected with an error before exec is called, rather
// than reaching workerd, which silently stringifies them ("1,x",
// "[object Object]"). []byte is a slice but a valid BLOB binding, so it must
// still be accepted.
func TestSQLStorage_Exec_RejectsSlicesAndMaps(t *testing.T) {
	calls := 0
	fake := js.ValueOf(map[string]any{})
	fake.Set("exec", js.FuncOf(func(this js.Value, args []js.Value) any {
		calls++
		return js.ValueOf(map[string]any{})
	}))
	s := SQLStorageFromJS(fake)

	for name, v := range map[string]any{
		"[]any":          []any{int64(1), "x"},
		"[]string":       []string{"a"},
		"[2]int":         [2]int{1, 2},
		"map[string]any": map[string]any{"a": int64(1)},
		"map[string]int": map[string]int{"a": 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Exec("INSERT INTO t VALUES (?)", v); err == nil {
				t.Fatal("Exec accepted the binding, want an error")
			}
		})
	}
	if calls != 0 {
		t.Errorf("exec was called %d times despite the rejected bindings", calls)
	}

	t.Run("[]byte is accepted", func(t *testing.T) {
		if _, err := s.Exec("INSERT INTO t VALUES (?)", []byte{1}); err != nil {
			t.Fatalf("Exec rejected a []byte binding: %v", err)
		}
		if calls != 1 {
			t.Errorf("exec was called %d times, want 1", calls)
		}
	})

	t.Run("no JS form returns an error, not a panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Exec panicked: %v", r)
			}
		}()
		if _, err := s.Exec("INSERT INTO t VALUES (?)", make(chan int)); err == nil {
			t.Fatal("Exec accepted a chan, want an error")
		}
	})
}
