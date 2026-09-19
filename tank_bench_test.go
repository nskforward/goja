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
