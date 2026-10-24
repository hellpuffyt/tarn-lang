# Security

Tarn runs programs that are often written by someone (or something) other
than the operator — that is the whole point of capability gating. This
document states what the runtime enforces and what it does not.

## Guarantees (tested)

| Property | Mechanism | Test |
|---|---|---|
| A program cannot reach the network, disk, environment, clock or RNG unless the operator allows that namespace | Every host effect is a tool; `ToolHost.Call` checks `Allowed[capability]` before invoking; there are no other effectful builtins | `TestToolsCapabilitiesTraceAndReplay` (denied then allowed) |
| Tool arguments and results match their declared types | Checked at compile time and again at the call boundary; host return values are checked before the program sees them | same ("returned the wrong type"), `TestStaticTypeErrors` |
| A replay cannot be misused by a different program | The replayer requires the same tool, in the same order, with structurally equal arguments, and refuses otherwise | same ("replay: expected call #1") |
| A program cannot run forever or recurse without bound | `MaxSteps` counts statements and loop iterations; `MaxDepth` bounds calls | `TestRuntimeErrorsHavePositions` |
| Malformed or hostile source cannot crash the runtime | Positioned errors from every stage; 8,000 random token programs and mutation cases never panic; nesting is capped at 1000 levels so deep input cannot exhaust the Go stack; typed builtins re-check values that arrive through `Any` | `TestFuzzNeverPanics`, `TestParserAndLexerErrors`, `TestParserBoundsNesting`, `TestBuiltinsRecheckAnyArgumentsAtRuntime` |
| `par_map` cannot corrupt state or escape limits | Workers run Tarn code under an interpreter lock (released only during tool calls) and share the root step budget | `TestParMapIsSafeBoundedAndOverlapsTools` (under `-race`) |
| Deterministic execution | Cooperative FIFO tasks, seeded RNG, time as a tool | `TestTasksAreDeterministicAndDrained` (20 identical runs), seeded-run equality |
| Memory safety | Go, no `unsafe`, no cgo, standard library only | CI |

## Non-guarantees

- **`--allow` is coarse.** `fs` grants read/write/list of any path the
  process can reach; `http` grants any URL. Path allow-lists and URL
  allow-lists are on the roadmap. Run untrusted programs with the minimum
  set, in a container, as a low-privilege user.
- **`par_map` runs host tools concurrently.** Tool implementations must be
  safe to call from multiple goroutines (the built-in ones are).
- **Traces contain the data that flowed through tools** — file contents,
  HTTP bodies. `--trace` creates the file with mode 0600, but treat trace
  files as sensitive anyway (an existing file keeps its mode).
- **Resource use.** `range` is capped at 10 million elements and HTTP
  bodies at 8 MiB, but a program can still allocate large lists within
  its step budget. Set `MaxSteps` when embedding.
- **Timing side channels** are not considered.

## Reporting

Open a private security advisory at
https://github.com/hellpuffyt/tarn-lang/security/advisories/new with the
`.tarn` program and the command line. Acknowledgement within 7 days.
