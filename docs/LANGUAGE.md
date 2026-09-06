# The Tarn language

## Files and modules

A file is a module: `import`s and `tool` declarations at the top, then
functions and top-level statements. Top-level statements run once when the
module loads (before `main`). Every function is exported.

```
import "lib/text.tarn"              // bound as `text`
import "lib/text.tarn" as t         // or an alias
tool http.get(url: Str) -> Str      // binds a host tool; the name must exist on the host

fn main() { ... }                   // entry point of the program you `tarn run`
```

Statements end at a newline (or `}`); there are no semicolons.

## Types

| Type | Values |
|---|---|
| `Int` | 64-bit signed integers (wrapping) |
| `Str` | UTF-8 strings; indexing and `len` are by code point |
| `Bool` | `true`, `false` |
| `Nil` | `nil`; also the return type of functions without `->` |
| `List[T]` | `[1, 2, 3]` — ordered, mutable, `push`/`pop`/index |
| `Map[T]` | `{"k": v}` — string keys, iteration in sorted key order |
| `Fn(A, B) -> T` | closures and named functions |
| `Task[T]` | the result of `spawn f(...)` |
| `Any` | unannotated values; checked at typed boundaries at run time |

Annotations are optional on `let` and on parameters/returns; what is
annotated is enforced statically. Literals infer their element types
(`[1, 2]` is `List[Int]`; mixed lists are `List[Any]`).

## Statements

```
let x = expr                 let y: List[Int] = []
x = expr                     l[i] = v         m["k"] = v
if c { } else if d { } else { }
for item in list { }         for ch in "str" { }        for key in map { }
while c { break / continue }
return expr                  return
```

## Expressions

Precedence, low to high: `or`/`||` · `and`/`&&` · `== !=` · `< <= > >=` ·
`+ -` · `* / %` · unary `- ! not` · calls, indexing, `.field`.
`+` works on Int, Str (concatenation) and List (concatenation). `==`
compares structurally. `and`/`or` short-circuit. Integer division truncates;
`/ 0` is a runtime error.

```
fn(x: Int) -> Int { return x * 2 }      // closure; captures its scope
f(1, 2)    l[0]    m["k"]    text.words(s)    http.get(url)
spawn work(1)         await task         await [t1, t2]
```

## Tasks

`spawn f(args)` evaluates arguments now and queues the call. `await t`
runs queued tasks **in spawn order** until `t` finishes; `await [tasks]`
returns a list of results. Tasks never interleave — a task runs to
completion (including tasks it awaits itself). Tasks spawned but never
awaited run at program exit. An `error(...)` inside a task surfaces at its
`await` (or at exit).

`par_map(list, fn)` runs `fn` on every element in parallel goroutines and
returns results in input order; use it for I/O fan-out.

## Tools and capabilities

| Tool | Capability | Signature |
|---|---|---|
| `http.get` | `http` | `(url: Str) -> Str` |
| `fs.read` / `fs.write` / `fs.list` | `fs` | `(path) -> Str` / `(path, body)` / `(dir) -> List[Str]` |
| `env.get` | `env` | `(name: Str) -> Str` |
| `time.now` | `time` | `() -> Int` (Unix milliseconds) |
| `rand.int` | `rand` | `(bound: Int) -> Int`, seeded by `--seed` |
| `proc.args` | `proc` | `() -> List[Str]` (arguments after `--`) |

Declare before use with the types you expect; the runtime verifies both the
arguments you pass and the value the host returns. Run with
`--allow http,fs`; a call without its capability fails with a positioned
error naming the flag.

## Trace and replay

`--trace out.json` records every tool call. `--replay in.json` runs the
program against the recording: no capabilities are needed, no host tool
runs, and if the program calls a different tool, or the same tool with
different arguments, or more calls than recorded, execution stops with
`replay: expected call #N …`.

## Builtins

`print(...)` `len(x)` `str(...)` `int(x)` `type(x)` `push(l, v)` `pop(l)`
`keys(m)` `values(m)` `has(m, k)` `range(n)`/`range(a, b)` `join(l, sep)`
`split(s, sep)` `contains(s_or_l, x)` `upper/lower/trim(s)` `map(l, f)`
`filter(l, f)` `reduce(l, init, f)` `sort(l)` `slice(x, a, b)` `all(tasks)`
`assert(c, msg)` `error(msg)` `min/max(a, b)` `abs(n)` `json(v)`
`parse_json(s)` `par_map(l, f)`.

## Errors

Compile-time errors (`tarn check`, and before any run) report
`file:line:col: message`. Runtime errors do the same and exit 1. There is
no exception handling yet; `error(msg)` unwinds to the nearest `await` or
to `main`.

## Formatting

`tarn fmt file.tarn` prints the canonical form; `-w` rewrites the file;
without `-w` the exit code is 3 if the file would change (for CI).
