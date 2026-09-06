# Architecture

```
 source ─► Lex ─► Parse ─► Module ─► Check (gradual types) ─► Interp
                                          ▲                     │
                                imports: load → check → init    ├─ Env / Closure / List / Map
                                                                ├─ Task queue (FIFO, cooperative)
                                                                └─ ToolHost: caps · trace · replay
```

## Files

| File | Role |
|---|---|
| `lexer.go` | tokens with 1-based positions; newlines are tokens (statement terminators) |
| `ast.go` | types, expressions, statements, `Module` |
| `parser.go` | recursive descent; precedence climbing; `else if`; newline-terminated statements |
| `types.go` | gradual checker producing `[]*Error`; `ModuleTypes` for imports |
| `interp.go` | values, environments, evaluation, tasks, modules, runtime type checks |
| `tools.go` | `ToolHost` (registry, capabilities, trace, replay), all builtins |
| `format.go` | canonical printer from the AST |
| `cmd/tarn` | `run`, `check`, `fmt`, `repl` |

## Type checking

`Check(module, imports)` walks every function body with a scope stack.
Rules are ordinary (operators, conditions, arity, returns) with one design
choice: **`Any` is compatible with everything**, and only appears where the
programmer left something unannotated or used a polymorphic builtin. Tool
declarations are never `Any`, so tool calls are always fully checked. The
checker returns *all* errors; the CLI prints them in order.

Because `Any` can smuggle a wrong value into a typed function, `Interp.call`
re-checks arguments against declared parameter types at run time
(`checkValue`), and `ToolHost.Call` checks both arguments and the host's
return value. The static check catches what it can early; the dynamic
check guarantees the declared types are true in practice.

## Evaluation

A straightforward tree-walking evaluator over Go values (`int64`, `string`,
`bool`, `nil`, `*List`, `*Map`, `*Closure`, `*Task`, …). Control flow uses
a returned `ctrl` signal rather than panics. `Env` is a chain of maps;
closures capture their defining `Env`, so `counter()` in the tests works.
Every statement increments a step counter; `MaxSteps` and `MaxDepth` make
runaway programs stop with a positioned error.

## Tasks

`spawn f(args)` evaluates the callee and arguments immediately and pushes a
`Task` onto a FIFO queue. `await t` pops and runs tasks **in spawn order**
until `t` is done; each task runs to completion (a task that awaits another
recursively drives the queue). At program end, `drain` runs whatever was
never awaited. There are no goroutines in this path, so output order is a
pure function of the program.

`par_map(list, fn)` is the explicit escape hatch: real goroutines, results
gathered in input order, `print` serialised by a mutex. Its trace entries
are ordered by completion.

## Tool host

`Register(name, capability, impl)` binds a host function. A program must
declare a tool (`tool ns.name(...) -> T`) to call it; the declaration must
name a registered tool. `Call`:

1. checks argument count and types against the declaration;
2. in **replay** mode, pops the next recorded entry and requires the same
   tool name and structurally equal arguments, then returns the recorded
   result or error;
3. otherwise requires `Allowed[capability]` and invokes the host function;
4. appends a `TraceEntry {seq, tool, args, result|error}`;
5. checks the result against the declared return type.

Traces are JSON; values round-trip through `toJSON`/`fromJSON` (integers
survive as `int64`).

`time.now` and `rand.int` are ordinary tools, so a replay reproduces time
and randomness too; `Seed` drives an xorshift generator for `rand.int`.

## Modules

`import "path.tarn" as alias` loads relative to the importing file, checks
it, runs its top-level statements once, and exposes its functions as
`alias.name`. Modules are cached by absolute path; a module is registered
before its imports resolve so cycles are reported instead of recursed.

## Formatter

`Format` prints the AST in canonical form: 4-space indentation, one
statement per line, spaces around binary operators, parentheses only where
precedence requires them, `else if` chains flattened. Because it consumes
the same AST the interpreter does, idempotence and meaning preservation are
testable properties (and tested on every example).

## Deliberate simplifications

| Simplification | Upgrade path |
|---|---|
| Tree-walking evaluation | compile to a register bytecode; the AST and checker stay |
| Cooperative tasks only | a scheduler with explicit yield points at tool calls, keeping FIFO determinism |
| Replay matches by sequence | key entries by (tool, args) hash for out-of-order matching under `par_map` |
| Errors unwind to `await`/`main` | `try`/`catch` or result values |
