package tarn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, src string, setup func(*Interp)) (string, error) {
	t.Helper()
	var out strings.Builder
	in := NewInterp()
	in.Out = func(s string) { out.WriteString(s) }
	in.MaxSteps = 5_000_000
	if setup != nil {
		setup(in)
	}
	err := in.RunSource("test.tarn", src)
	return out.String(), err
}

func mustRun(t *testing.T, src string) string {
	t.Helper()
	out, err := run(t, src, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, src)
	}
	return out
}

func mustFail(t *testing.T, src, want string) {
	t.Helper()
	_, err := run(t, src, nil)
	if err == nil {
		t.Fatalf("expected error containing %q, got none\n%s", want, src)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %q", want, err)
	}
}

func TestBasicsAndControlFlow(t *testing.T) {
	out := mustRun(t, `
fn fact(n: Int) -> Int {
    if n <= 1 { return 1 }
    return n * fact(n - 1)
}
fn main() {
    let mut_list = [3, 1, 2]
    print(fact(5), sort(mut_list), len("héllo"))
    let i = 0
    let acc = []
    while true {
        i = i + 1
        if i % 2 == 0 { continue }
        if i > 7 { break }
        push(acc, i)
    }
    print(acc, join(acc, "-"))
    for c in "ab" { print(c) }
    for k in {"b": 1, "a": 2} { print(k) }
    print(1 == 1 and not false or false, "x" + "y", [1] + [2], -(3 - 5))
    print({"k": [1, {"n": nil}]}, type(nil), int("42") + int(true))
}`)
	want := "120 [1, 2, 3] 5\n[1, 3, 5, 7] 1-3-5-7\na\nb\na\nb\ntrue xy [1, 2] 2\n{\"k\": [1, {\"n\": nil}]} Nil 43\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestClosuresAndHigherOrder(t *testing.T) {
	out := mustRun(t, `
fn counter() -> Fn() -> Int {
    let n = 0
    return fn() -> Int { n = n + 1
        return n }
}
fn main() {
    let c = counter()
    c()
    c()
    print(c())
    let sq = map(range(1, 5), fn(x: Int) -> Int { return x * x })
    print(sq, filter(sq, fn(x: Int) -> Bool { return x % 2 == 0 }), reduce(sq, 0, fn(a: Int, b: Int) -> Int { return a + b }))
    print(slice("hello", 1, 3), slice([1, 2, 3], 0, 2), contains([1, 2], 2), has({"a": 1}, "b"))
}`)
	if out != "3\n[1, 4, 9, 16] [4, 16] 30\nel [1, 2] true false\n" {
		t.Fatalf("got %q", out)
	}
}

func TestTasksAreDeterministicAndDrained(t *testing.T) {
	src := `
fn work(id: Int) -> Int {
    print("start", id)
    if id == 1 { let inner = spawn work(10)
        print("inner", await inner) }
    return id * 2
}
fn main() {
    let a = spawn work(1)
    let b = spawn work(2)
    let c = spawn work(3)
    print("spawned")
    print(await b)
    print(await [a, c])
    spawn work(99)
    print("end of main")
}`
	// FIFO: awaiting b runs a first; a's inner task queues behind 2 and 3.
	want := "spawned\nstart 1\nstart 2\nstart 3\nstart 10\ninner 20\n4\n[2, 6]\nend of main\nstart 99\n"
	for i := 0; i < 20; i++ {
		if out := mustRun(t, src); out != want {
			t.Fatalf("run %d differs:\n%s\nwant:\n%s", i, out, want)
		}
	}
	mustFail(t, `fn boom() { error("kaboom") } fn main() { let t = spawn boom()
	print("x")
	await t }`, "kaboom")
}

func TestStaticTypeErrors(t *testing.T) {
	cases := map[string]string{
		`fn main() { let x: Int = "s" }`:                         "cannot initialise Int with a value of type Str",
		`fn f(a: Int) -> Int { return a } fn main() { f("x") }`:  "argument 1 must be Int, found Str",
		`fn f(a: Int) -> Int { return a } fn main() { f(1, 2) }`: "expected 1 argument(s), got 2",
		`fn f() -> Int { if true { return 1 } } fn main() { }`:   "not every path returns",
		`fn main() { let x = 1 + "a" }`:                          "cannot add Int and Str",
		`fn main() { if 1 { } }`:                                 "condition must be Bool",
		`fn main() { print(y) }`:                                 "unknown name \"y\"",
		`fn main() { let l: List[Int] = [1]
		l[0] = "s" }`: "list element must be Int",
		`fn main() { break }`:               "break outside a loop",
		`fn main() { let t = spawn 5 }`:     "spawn needs a function call",
		`fn f() { return 1 } fn main() { }`: "returning a value from a function that returns Nil",
		`fn main() { http.get("x") }`:       "unknown tool or module",
		`tool http.get(url: Int) -> Str
		fn main() { http.get("x") }`: "argument 1 must be Int, found Str",
		`tool nope.thing()
		fn main() { }`: "no host tool named",
		`fn main() { let x = 1
		let x = 2 }`: "already declared",
		`fn f() {} fn f() {} fn main() {}`:           "defined twice",
		`fn main() { await 3 }`:                      "await needs a Task",
		`fn main() { let m: Map[Int] = {"a": "b"} }`: "cannot initialise Map[Int]",
	}
	for src, want := range cases {
		mustFail(t, src, want)
	}
}

func TestRuntimeErrorsHavePositions(t *testing.T) {
	_, err := run(t, "fn main() {\n    let l = [1]\n    print(l[3])\n}", nil)
	if err == nil || !strings.Contains(err.Error(), "test.tarn:3:12: index 3 out of range") {
		t.Fatalf("got %v", err)
	}
	mustFail(t, `fn main() { print(1 / 0) }`, "division by zero")
	mustFail(t, `fn f(x: Int) -> Int { return f(x) } fn main() { f(1) }`, "stack overflow")
	mustFail(t, `fn main() { let m = {"a": 1}
	print(m["z"]) }`, "missing key")
	// Values entering through Any are checked at the callee.
	mustFail(t, `fn f(a: Int) -> Int { return a } fn main() { let any = parse_json("\"s\"")
	f(any) }`, "argument \"a\": expected Int, got Str")
	mustFail(t, `fn main() { assert(1 == 2, "math") }`, "assertion failed: math")
	_, err = run(t, `fn main() { while true { } }`, func(in *Interp) { in.MaxSteps = 1000 })
	if err == nil || !strings.Contains(err.Error(), "step limit") {
		t.Fatalf("step limit: %v", err)
	}
}

func TestToolsCapabilitiesTraceAndReplay(t *testing.T) {
	src := `
tool fs.read(path: Str) -> Str
tool rand.int(bound: Int) -> Int
fn main() {
    print(len(fs.read("x")), rand.int(100), rand.int(100))
}`
	// Denied without the capability.
	_, err := run(t, src, func(in *Interp) {
		in.Tools.Register("fs.read", "fs", func(a []any) (any, error) { return "hello", nil })
	})
	if err == nil || !strings.Contains(err.Error(), `capability "fs" is required`) {
		t.Fatalf("expected capability error, got %v", err)
	}
	// Allowed: runs, and the trace records every call.
	var host *ToolHost
	out, err := run(t, src, func(in *Interp) {
		in.Tools.Register("fs.read", "fs", func(a []any) (any, error) { return "hello", nil })
		in.Tools.Allowed["fs"] = true
		in.Tools.Allowed["rand"] = true
		in.Tools.Seed = 42
		host = in.Tools
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(host.Trace) != 3 || host.Trace[0].Tool != "fs.read" || host.Trace[1].Tool != "rand.int" {
		t.Fatalf("trace: %+v", host.Trace)
	}
	// Same seed → same output.
	out2, _ := run(t, src, func(in *Interp) {
		in.Tools.Register("fs.read", "fs", func(a []any) (any, error) { return "hello", nil })
		in.Tools.Allowed["fs"] = true
		in.Tools.Allowed["rand"] = true
		in.Tools.Seed = 42
	})
	if out != out2 {
		t.Fatalf("seeded runs differ: %q vs %q", out, out2)
	}
	// Replay: no capabilities, no real tool, identical output.
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.json")
	if err := host.SaveTrace(path); err != nil {
		t.Fatal(err)
	}
	out3, err := run(t, src, func(in *Interp) {
		in.Tools.Register("fs.read", "fs", func(a []any) (any, error) { t.Fatal("real tool called during replay"); return nil, nil })
		if err := in.Tools.LoadReplay(path); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil || out3 != out {
		t.Fatalf("replay: %v %q vs %q", err, out3, out)
	}
	// A diverging program is refused.
	_, err = run(t, strings.Replace(src, `fs.read("x")`, `fs.read("y")`, 1), func(in *Interp) {
		in.Tools.Register("fs.read", "fs", func(a []any) (any, error) { return "", nil })
		_ = in.Tools.LoadReplay(path)
	})
	if err == nil || !strings.Contains(err.Error(), "replay: expected call #1") {
		t.Fatalf("divergence not detected: %v", err)
	}
	// Tool return type is enforced against the declaration.
	_, err = run(t, src, func(in *Interp) {
		in.Tools.Register("fs.read", "fs", func(a []any) (any, error) { return int64(5), nil })
		in.Tools.Allowed["fs"] = true
		in.Tools.Allowed["rand"] = true
	})
	if err == nil || !strings.Contains(err.Error(), "returned the wrong type") {
		t.Fatalf("return type: %v", err)
	}
}

func TestModulesImportAndCycles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "lib.tarn"), []byte("fn twice(x: Int) -> Int { return x * 2 }\nlet loaded = 1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.tarn"), []byte("import \"lib.tarn\" as l\nfn main() { print(l.twice(21)) }\n"), 0o644)
	var out strings.Builder
	in := NewInterp()
	in.Out = func(s string) { out.WriteString(s) }
	if err := in.RunFile(filepath.Join(dir, "main.tarn")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "42\n" {
		t.Fatalf("got %q", out.String())
	}
	os.WriteFile(filepath.Join(dir, "bad.tarn"), []byte("import \"lib.tarn\"\nfn main() { print(lib.nope()) }\n"), 0o644)
	if err := NewInterp().RunFile(filepath.Join(dir, "bad.tarn")); err == nil || !strings.Contains(err.Error(), `module "lib" has no function "nope"`) {
		t.Fatalf("got %v", err)
	}
	os.WriteFile(filepath.Join(dir, "a.tarn"), []byte("import \"b.tarn\"\nfn main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.tarn"), []byte("import \"a.tarn\"\n"), 0o644)
	if err := NewInterp().RunFile(filepath.Join(dir, "a.tarn")); err == nil || !strings.Contains(err.Error(), "import cycle") {
		t.Fatalf("cycle: %v", err)
	}
}

func TestFormatterIsIdempotentAndPreservesMeaning(t *testing.T) {
	entries, _ := filepath.Glob("examples/*.tarn")
	libs, _ := filepath.Glob("examples/lib/*.tarn")
	for _, f := range append(entries, libs...) {
		src, _ := os.ReadFile(f)
		m, err := Parse(f, string(src))
		if err != nil {
			t.Fatal(err)
		}
		once := Format(m)
		m2, err := Parse(f, once)
		if err != nil {
			t.Fatalf("%s: formatted output does not parse: %v\n%s", f, err, once)
		}
		if twice := Format(m2); twice != once {
			t.Fatalf("%s: formatting is not idempotent", f)
		}
	}
	ugly := "fn   main( ) {\n let x=1+2*3\n if x>5{print( x )}else{print(0)}\n}\n"
	m, _ := Parse("u", ugly)
	want := "fn main() {\n    let x = 1 + 2 * 3\n    if x > 5 {\n        print(x)\n    } else {\n        print(0)\n    }\n}\n"
	if got := Format(m); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	// Precedence survives: (1 + 2) * 3 keeps its parentheses.
	m, _ = Parse("p", "fn main() { print((1 + 2) * 3, 1 + 2 * 3, -(1 - 2)) }")
	if got := Format(m); !strings.Contains(got, "print((1 + 2) * 3, 1 + 2 * 3, -(1 - 2))") {
		t.Fatalf("precedence lost: %s", got)
	}
}

func TestExamplesMatchExpectations(t *testing.T) {
	for _, f := range []string{"examples/hello.tarn", "examples/agents.tarn"} {
		src, _ := os.ReadFile(f)
		var want strings.Builder
		for _, line := range strings.Split(string(src), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "// expect:"); ok {
				want.WriteString(strings.TrimSpace(rest) + "\n")
			}
		}
		var out strings.Builder
		in := NewInterp()
		in.Out = func(s string) { out.WriteString(s) }
		if err := in.RunFile(f); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if out.String() != want.String() {
			t.Fatalf("%s:\n%s\nwant:\n%s", f, out.String(), want.String())
		}
	}
}

func TestParserAndLexerErrors(t *testing.T) {
	cases := map[string]string{
		"fn main() { let x = \"open }":               "unterminated string",
		"fn main() { let x = 1 $ 2 }":                "unexpected character",
		"fn main() { let = 1 }":                      "expected identifier",
		"fn main() { if x { ":                        "block is never closed",
		"fn main() { let x: Nope = 1 }":              "unknown type",
		"fn main() { 1 + }":                          "expected an expression",
		"fn main() { tool x.y() }":                   "only allowed at the top level",
		"fn main() { let x = 99999999999999999999 }": "out of range",
		"fn main() { f() = 3 }":                      "cannot assign",
	}
	for src, want := range cases {
		_, err := Parse("t", src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: want %q, got %v", src, want, err)
		}
	}
}

func TestFuzzNeverPanics(t *testing.T) {
	atoms := []string{"fn", "main", "(", ")", "{", "}", "let", "x", "=", "1", "\"s\"", "+", "if", "else", "for", "in", "while", "return", "spawn", "await", "[", "]", ",", ":", "Int", "->", "print", "tool", "a", ".", "b", "\n", "true", "and", "not", "range", "5"}
	seed := uint64(12345)
	next := func() uint64 { seed ^= seed << 13; seed ^= seed >> 7; seed ^= seed << 17; return seed }
	for i := 0; i < 8000; i++ {
		n := 1 + int(next()%25)
		var b strings.Builder
		for k := 0; k < n; k++ {
			b.WriteString(atoms[next()%uint64(len(atoms))])
			b.WriteByte(' ')
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on %q: %v", b.String(), r)
				}
			}()
			in := NewInterp()
			in.Out = func(string) {}
			in.MaxSteps = 2000
			in.MaxDepth = 50
			_ = in.RunSource("fuzz", b.String())
		}()
	}
}
