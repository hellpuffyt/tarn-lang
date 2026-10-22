# Production readiness — tarn-lang

Audit of the `audit/production-readiness` branch. Evidence only; the
classification at the end is a proposal for the maintainer to confirm.

- Tested commit: `008d86dcc4ebc57f0e5ab4ec497d3d0ce1bf5243` (code; this
  file was added in the next commit and changes no code)
- Date: 2026-10-22
- Host: macOS 27 (Apple Silicon), Go 1.27.3. `go.mod` declares `go 1.24`.

## Scope

Tarn is a small interpreted language for agent workflows: a gradual static
checker, a tree-walking interpreter with deterministic cooperative tasks,
capability-gated typed tools (`http`, `fs`, `env`, `time`, `rand`, `proc`),
trace recording and verifying replay, an idempotent formatter and a REPL.
Single Go package plus `cmd/tarn`; standard library only. See
[`ARCHITECTURE.md`](../ARCHITECTURE.md).

Out of scope (documented as limits in the README): floats, structs,
exceptions, comment-preserving formatting, path/URL allow-lists.

## Platforms and prerequisites

- Go 1.24 or newer. Verified locally on macOS (arm64) with Go 1.27.3.
- CI matrix covers ubuntu, macOS and Windows with Go 1.24; that matrix has
  not yet run on this branch.

## Clean-clone verification

Fresh `git clone` of the branch into an empty scratch directory, README
"Build, test, run" followed literally:

| Command | Exit | Result |
|---|---|---|
| `go build -o tarn ./cmd/tarn` | 0 | |
| `go test -race ./...` | 0 | 16 passed, 0 failed, 0 skipped |
| `./tarn run examples/hello.tarn` | 0 | `hello, Tarn` / `55` / `{"evens": [0, 2, 4], "odds": [1, 3]}` |
| `./tarn run examples/agents.tarn` | 0 | ends with `total 5` |
| `mkdir -p examples/out` | 0 | (step added by this audit; see bugs) |
| `./tarn run examples/pipeline.tarn --allow fs,time,rand --seed 7 --trace run.json` | 0 | `summary: 122 words across 2 files; sample examples/hello.tarn` |
| `./tarn run examples/pipeline.tarn --replay run.json` | 0 | identical output, no capabilities |
| `./tarn check examples/agents.tarn && ./tarn fmt examples/agents.tarn` | 3 | check ok; fmt exit 3 = "would change" because the formatter drops comments (documented limit) |

`run.json` was created with mode `-rw-------`.

The README hero program was also run with its URL pointed at a local
`python3 -m http.server` on 127.0.0.1: without `--allow` it fails with
`flow.tarn:5:31: capability "http" is required ...` (exit 1); with
`--allow http,fs --trace run.json` it prints `3 items`; `--replay run.json`
prints `3 items` again after the server was stopped.

## Commands executed (working clone)

| Command | Exit | Outcome |
|---|---|---|
| `go build ./...` | 0 | |
| `go vet ./...` | 0 | |
| `gofmt -l .` | 0 | no output |
| `go test -race -count=1 ./...` (baseline, before fixes) | 0 | 11 passed, 0 failed, 0 skipped |
| `go test -race -count=1 ./...` (after) | 0 | 16 passed, 0 failed, 0 skipped |
| `go test -race -count=5 ./...` | 0 | stable across 5 runs |
| `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` (v0.8.2) | 0 | no findings |
| `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` (v1.9.0) | 0 | No vulnerabilities found (no dependencies) |
| `go run github.com/zricethezav/gitleaks/v8@latest git --redact .` | 0 | no leaks in any commit |
| CI "real execution" script, run locally under `bash -eo pipefail` | 0 | after the SIGPIPE fix below; 30/30 repeated REPL runs pass |

Every new regression test was also run against the pre-fix code and
failed there (panic, `fatal error: concurrent map read and map write`,
missing error, or wrong mode).

## CI status

- Default branch (`main`): last run `success`.
- Audit branch: pending until pushed.

## Bugs found and fixed

| # | Severity | Bug | Fix | Regression test |
|---|---|---|---|---|
| 1 | High (crash, sandbox bypass) | `par_map` workers evaluated Tarn code concurrently on shared environments: `fatal error: concurrent map writes` (uncatchable process kill). Workers also ran with no step budget (escaping `MaxSteps`) and silently dropped tasks they spawned. | Interpreter lock around worker evaluation, released only during tool calls; workers charge the root step budget and drain their task queue. | `TestParMapIsSafeBoundedAndOverlapsTools` |
| 2 | Medium (advertised behaviour) | The tool host held its mutex across the host call, so `par_map` tool calls (the I/O it exists for) ran one at a time. | Lock only around trace/replay bookkeeping; RNG has its own lock. | same test: 4 tool calls observed in flight at once |
| 3 | High (crash) | A value reaching a typed builtin through `Any` (e.g. `upper(parse_json("1"))`, `map([1], upper)`) panicked the Go runtime. | Builtins with signatures re-check arguments at call time and return a positioned error. | `TestBuiltinsRecheckAnyArgumentsAtRuntime` |
| 4 | High (crash) | Deep nesting (parens, unary, long operator chains, blocks, types, else-if chains) overflowed the Go stack — uncatchable. | Parser nesting limit of 1000 with a positioned error. | `TestParserBoundsNesting` |
| 5 | Low | `json(f)` on a function serialised the interpreter's AST. | Error for functions, tasks, modules. | `TestJSONRejectsNonData` |
| 6 | Low (data exposure) | `--trace` files (which hold file bodies, HTTP bodies, env values) were created world-readable (0644). | Created 0600. | `TestSaveTraceIsOwnerOnly` (skipped on Windows) |
| 7 | CI flake | `./tarn repl \| grep -q 42` under GitHub's `bash -eo pipefail` intermittently exits 141 (SIGPIPE) — reproduced 1 in 5 locally. | `grep 42 > /dev/null`; actions pinned to commit SHAs. | 30/30 local runs |
| 8 | Docs | README quick start failed in a fresh clone (`examples/out` missing); hero error position was wrong (`6:23` vs real `5:31`); "20 static-error cases" (there are 18); line count understated. | README corrected. | clean-clone run above |

## Security review

- Capability gate: every effect is a tool; `ToolHost.Call` checks the
  capability before the host function runs; replay needs none. Unchanged.
- Resource exhaustion: step limit (when embedding), depth limit, `range`
  cap, 8 MiB HTTP body cap, 30 s HTTP timeout, and now a parser nesting
  limit and par_map step accounting.
- SSRF / path traversal: `http` and `fs` capabilities are coarse by design
  (any URL / any path the process can reach). Documented in `SECURITY.md`;
  run untrusted programs with the minimum `--allow` set.
- Secrets: gitleaks over all history found nothing; no tokens or keys in
  the tree.
- Workflow permissions: `contents: read`; actions now pinned to SHAs.

## Known limitations and unresolved risks

- `tarn run` has no step-limit flag; `while true {}` runs until killed.
  `MaxSteps` is only settable when embedding.
- Replay does not fail a program that makes fewer tool calls than
  recorded.
- `parse_json` of a non-integer number yields a value arithmetic rejects.
- `tarn fmt` drops comments.
- Windows and Linux were not run locally; they rely on CI after push.
- `http.get` was exercised only against a local HTTP server.

## Acceptance checklist

| Item | Expected behaviour | Command / test | Result | Actual | Reference |
|---|---|---|---|---|---|
| Capability gate | Tool call without `--allow` fails naming the flag | `TestToolsCapabilitiesTraceAndReplay`; hero example run | pass | `capability "http" is required ... (run with --allow http)` | `tools.go` `Call` |
| Trace + offline replay | Replay output identical, no capabilities | clean-clone pipeline run + replay | pass | identical two lines | `tools.go` |
| Replay divergence | Different call refused | `TestToolsCapabilitiesTraceAndReplay` | pass | `replay: expected call #1 ...` | `tools.go` |
| Deterministic tasks | Same output over 20 runs; unawaited tasks drained | `TestTasksAreDeterministicAndDrained` | pass | | `interp.go` |
| Static checker | Positioned compile errors | `TestStaticTypeErrors` (18 cases) | pass | | `types.go` |
| Hostile input cannot crash | No panic / stack overflow | `TestFuzzNeverPanics`, `TestParserBoundsNesting`, `TestBuiltinsRecheckAnyArgumentsAtRuntime` | pass | | `parser.go`, `interp.go` |
| `par_map` safety | No races, step limit holds, tool I/O overlaps | `TestParMapIsSafeBoundedAndOverlapsTools` under `-race` | pass | 500/500 updates, peak 4 concurrent tool calls | `tools.go` |
| Formatter | Idempotent, meaning-preserving | `TestFormatterIsIdempotentAndPreservesMeaning` | pass | | `format.go` |
| Static analysis / deps | vet, staticcheck, govulncheck clean | commands above | pass | no findings | |
| Fresh clone | README steps work | clean-clone table | pass | after README fix | `README.md` |

## Classification proposal

**Production-Ready for Its Documented Scope** — proposed, pending
independent verification and the CI matrix (Linux, Windows) on this
branch. The documented scope is an experimental 0.1.0 language runtime;
the limitations above are part of that scope.
