# Testing

`go test -race ./...` — one package, ~3 seconds.

| Test | What it proves |
|---|---|
| `TestBasicsAndControlFlow` | Recursion, `while` with `break`/`continue`, `for` over strings and maps (sorted keys), `and`/`or`/`not`, string and list `+`, map/nil printing, `int` conversions, unicode `len`. |
| `TestClosuresAndHigherOrder` | Closures capture and mutate their environment; `map`/`filter`/`reduce`/`slice`/`contains`/`has`. |
| `TestTasksAreDeterministicAndDrained` | A nested spawn/await program prints the same 10 lines across 20 runs; an error in a never-awaited task surfaces at drain. |
| `TestStaticTypeErrors` | 18 compile-time diagnostics: initialisers, arguments, arity, every-path return, operators, conditions, unknown names, indexed assignment, `break` placement, `spawn` shape, tool typing, unknown tools, redeclaration, duplicate functions, `await` typing. |
| `TestRuntimeErrorsHavePositions` | `file:line:col` on runtime errors; division by zero; stack overflow; missing keys; a value that entered through `Any` is rejected at a typed parameter; `assert`; the step limit stops `while true {}`. |
| `TestToolsCapabilitiesTraceAndReplay` | Denied without capability; allowed with; trace has three entries in order; seeded runs are identical; replay produces identical output with the real tool disabled; a diverging program is refused; wrong host return types are rejected. |
| `TestModulesImportAndCycles` | `import … as`, calling module functions, unknown member error, import cycle detection. |
| `TestFormatterIsIdempotentAndPreservesMeaning` | Every example formats to itself after one pass; ugly input is canonicalised; precedence parentheses survive. |
| `TestExamplesMatchExpectations` | `examples/hello.tarn` and `examples/agents.tarn` print exactly their `// expect:` lines. |
| `TestParserAndLexerErrors` | Nine malformed inputs, each with the right message. |
| `TestFuzzNeverPanics` | 8,000 random token programs through lex/parse/check/run with step and depth limits — no panic. |

## Real execution in CI

The workflow builds `tarn`, runs the three examples, records a trace of
`pipeline.tarn` with capabilities, replays it *without* capabilities and
diffs the output, confirms the capability error without `--allow`, runs
`tarn check` and `tarn fmt` on every example (exit 3 = would reformat), and
pipes a session through `tarn repl`.

## Writing a test

Use `run(t, src, setup)` to execute source with captured output; register
fake tools in `setup` with `in.Tools.Register`. Prefer asserting on exact
printed output — the runtime's determinism makes that reliable.
