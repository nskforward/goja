package goja

import "testing"

// BenchmarkTankIteration mimics load-generator's hot path: a long-lived
// Runtime and iteration function called repeatedly from Go. It guards the
// value stack retention optimization (leave() must not re-allocate the stack).
func BenchmarkTankIteration(b *testing.B) {
	rt := New()
	_, err := rt.RunString(`var f = function () { var x = 1; return x; };`)
	if err != nil {
		b.Fatal(err)
	}
	fn, ok := AssertFunction(rt.Get("f"))
	if !ok {
		b.Fatal("not a function")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err = fn(Undefined()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTankJSONParse measures JSON.parse on a representative payload. It
// guards the direct recursive-descent parser against regressions.
func BenchmarkTankJSONParse(b *testing.B) {
	const data = `{"id":123,"name":"test","active":true,"score":3.14,"tags":["a","b","c"],"nested":{"x":1,"y":2.5,"z":null}}`
	rt := New()
	jsonObj, ok := rt.Get("JSON").(*Object)
	if !ok {
		b.Fatal("no JSON")
	}
	parse, ok := AssertFunction(jsonObj.Get("parse"))
	if !ok {
		b.Fatal("no JSON.parse")
	}
	arg := rt.ToValue(data)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parse(Undefined(), arg); err != nil {
			b.Fatal(err)
		}
	}
}
