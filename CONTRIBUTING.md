# Contributing

## Ground rules

- Standard library only; no `unsafe`, no cgo.
- **Determinism is the product.** No new source of nondeterminism may enter
  the interpreter (`interp.go`): time, randomness, map iteration order in
  anything observable, goroutines. Effects go through `ToolHost` as tools.
- Every new builtin gets a type signature in `builtinSigs` and a test.
  Every new tool gets a capability and a trace round-trip test.
- Every parser or formatter change keeps `TestFormatterIsIdempotent…` green.
- `gofmt -l .` empty, `go vet ./...` clean, `go test -race ./...` green.

## Workflow

```
git clone https://github.com/hellpuffyt/tarn-lang
cd tarn-lang
go test -race ./...
go build -o tarn ./cmd/tarn && ./tarn run examples/agents.tarn
```

## Where things live

| Want to… | Look in |
|---|---|
| Add syntax | `lexer.go`, `ast.go`, `parser.go`, then `format.go` (printer) and `docs/LANGUAGE.md` |
| Add a type rule | `types.go` (`Checker.expr` / `stmt`) and `checkValue` in `interp.go` for the runtime side |
| Add a builtin | `tools.go` (`def(...)`) + `builtinSigs` in `types.go` |
| Add a tool namespace | `NewToolHost` in `tools.go` (`Register(name, capability, impl)`) |
| Change scheduling or replay | `interp.go` (`spawn`/`await`/`drain`), `tools.go` (`Call`) |

## Reporting bugs

A `.tarn` file plus the command line. For replay issues attach the trace
JSON. Nondeterminism reports are the most valuable — include two differing
outputs.
