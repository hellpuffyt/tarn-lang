# Tarn

**A small language for agent workflows where every run is deterministic,
every side effect is a typed, capability-gated tool call, and every run can
be recorded and replayed without the outside world.**

```
tool http.get(url: Str) -> Str          // a typed tool; needs --allow http
tool fs.write(path: Str, body: Str)     // needs --allow fs

fn fetch(name: Str) -> Map[Any] {
    return parse_json(http.get("https://api.example.com/items/" + name))
}

fn main() {
    let tasks = map(["a", "b", "c"], fn(n: Str) -> Task[Map[Any]] { return spawn fetch(n) })
    let items = await tasks                    // tasks run in spawn order: no races, ever
    let report = join(map(items, fn(i: Map[Any]) -> Str { return str(i["title"]) }), "\n")
    fs.write("report.txt", report)
    print(len(items), "items")
}
```

```
$ tarn run flow.tarn
error: 6:23: capability "http" is required to call http.get (run with --allow http)

$ tarn run flow.tarn --allow http,fs --trace run.json
3 items

$ tarn run flow.tarn --replay run.json          # offline, no capabilities: identical output
3 items
```

Go, standard library only, ~2,600 lines. `tarn run | check | fmt | repl`.

## What is it?

| Piece | What it does |
|---|---|
| **Language** | `Int`, `Str`, `Bool`, `Nil`, `List[T]`, `Map[T]`, `Fn(..) -> T`, `Task[T]`, first-class closures, `if`/`for`/`while`, modules via `import`, string/list/map builtins, higher-order `map`/`filter`/`reduce`. |
| **Gradual static checker** | Annotated parameters, returns and `let` types are enforced at compile time (arity, operand types, every-path returns, break outside loop, unknown names, module members). Unannotated code is `Any` and re-checked at run time where it meets a typed boundary. |
| **Tools** | `tool ns.name(params) -> T` binds a host function. Arguments and results are checked against the declaration; each namespace needs a capability (`--allow http,fs,env,time,rand,proc`) or the call fails with a message that tells you which. |
| **Tasks** | `spawn f(x)` creates a task, `await t` / `await [t1, t2]` collects. Tasks are cooperative and run in spawn order, so output is identical on every machine. `par_map` gives real goroutine parallelism when you want it. |
| **Trace and replay** | `--trace out.json` records every tool call (sequence, tool, args, result). `--replay in.json` serves those results back and refuses to continue if the program diverges from the recording. Replays need no capabilities. |
| **Determinism** | `time.now` and `rand.int` are tools too; `--seed` fixes the RNG; there is no other source of nondeterminism. Same program + same trace = same run. |
| **Tooling** | `tarn check` (diagnostics with `file:line:col`), `tarn fmt` (idempotent canonical formatter, `-w` to write, exit 3 in CI if it would change), `tarn repl`. |

## Who is it for?

- Teams building **agent orchestration**, ETL, or automation where "why did
  it do that?" must have a reproducible answer.
- Anyone who wants a scripting language whose programs **cannot touch the
  network or disk unless the operator says so** — per namespace, on the
  command line.
- People studying **language implementation**: lexer, parser, checker,
  interpreter, scheduler, formatter and module loader in one readable
  codebase with a test suite that pins semantics.

## Why does it exist?

Agent workflows are written today in general-purpose languages where every
library call can hit the network, every goroutine reorders output, and
reproducing an incident means re-running against a world that has moved on.
Tarn makes the three properties that matter for that job *language-level*:
side effects are explicit and gated, scheduling is deterministic, and runs
are recordable. It is a small language on purpose — the interesting part is
the runtime contract, not the syntax.

## What makes it different?

- **Capabilities on the command line, per tool namespace**, enforced at the
  call site with a message naming the missing flag.
- **Typed tool boundaries in both directions**: arguments are checked
  against the declaration before the host sees them, and the host's return
  value is checked against the declared type before the program sees it.
- **Replay that verifies**: a recording is not just data to feed back; the
  replayer checks that the program makes the same calls in the same order
  with the same arguments, so a changed program cannot silently misuse an
  old trace.
- **Deterministic tasks without locks**: cooperative FIFO scheduling means
  `spawn`/`await` are reasoning tools, not hazards. Spawned tasks that were
  never awaited are still drained at exit, so no side effect is lost.
- **Formatter and checker built on the same parser**, so `tarn fmt` is
  guaranteed idempotent and meaning-preserving (tested on every example).

## Why is this not just a tutorial?

Because the runtime contract is tested: 20 static-error cases, runtime
errors with exact `line:col`, deterministic task output checked across 20
runs, capability denial, trace recording, seeded reproducibility, replay
equality, replay divergence detection, tool return-type enforcement, import
cycles, formatter idempotence over every example, a step limit that stops
`while true {}`, and an 8,000-program token-soup fuzz that must never panic —
all under `go test -race`.

## Build, test, run

```
go build -o tarn ./cmd/tarn
go test -race ./...
./tarn run examples/hello.tarn
./tarn run examples/agents.tarn
./tarn run examples/pipeline.tarn --allow fs,time,rand --seed 7 --trace run.json
./tarn run examples/pipeline.tarn --replay run.json
./tarn check examples/agents.tarn && ./tarn fmt examples/agents.tarn
```

Go 1.24+.

## Limits (honest)

- Tree-walking interpreter: fine for orchestration, wrong for number
  crunching (`examples/agents.tarn` runs in milliseconds; `fib(30)` takes
  seconds).
- Gradual typing means `Any` flows through builtins like `parse_json`; the
  runtime checks catch mismatches at typed boundaries, not before.
- Cooperative tasks do not run in parallel; `par_map` does, and its trace
  order follows completion order, so replaying a `par_map` run needs the
  same completion order.
- No floats, no structs, no pattern matching, no exceptions beyond
  `error(msg)` unwinding to the caller of `await`/`main`.

## Documentation

- [`docs/LANGUAGE.md`](docs/LANGUAGE.md) — syntax, types, builtins, tools, tasks, tracing
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — checker, interpreter, scheduler, tool host, replay protocol
- [`SECURITY.md`](SECURITY.md), [`TESTING.md`](TESTING.md), [`ROADMAP.md`](ROADMAP.md), [`CONTRIBUTING.md`](CONTRIBUTING.md), [`CHANGELOG.md`](CHANGELOG.md)

## Why star or contribute?

Star it if you have ever debugged a flaky agent pipeline and wished you could
just replay it. Contribute a tool namespace (SQL, S3, LLM calls), a bytecode
VM, floats, or an LSP — each lands against a small core with tests that say
exactly what must stay true.

## License

MIT. See [`LICENSE`](LICENSE).
