# Changelog

Format: [Keep a Changelog](https://keepachangelog.com). Versions: SemVer.
Language and trace-format changes are called out explicitly.

## [Unreleased]

### Fixed
- `tarn fmt` did not indent the body of an inline closure: the body was
  printed one level in from column 0 and the closing `}` at column 0,
  whatever the closure's nesting. Closure bodies now nest like function
  bodies (one level in from the line that opens them, closing brace aligned
  with that line), including closures inside closures.
- `par_map` no longer crashes with "concurrent map writes" when workers
  touch shared variables: worker code runs under an interpreter lock that is
  released around tool calls, so tool I/O now genuinely overlaps (the tool
  host previously held its lock during every host call).
- `par_map` workers share the program's step budget and drain tasks they
  spawn instead of silently dropping them.
- Typed builtins (`upper`, `min`, `split`, …) re-check arguments at run
  time; a value arriving through `Any` used to panic the interpreter.
- The parser caps nesting at 1000 levels; deeply nested input used to kill
  the process with an unrecoverable stack overflow.
- `json()` rejects functions, tasks and modules instead of dumping the AST.
- CI: actions pinned to commit SHAs; a `grep -q` SIGPIPE flake under
  `pipefail` removed.

### Security
- Trace files are written with mode 0600.

## [0.1.0] — 2026-09-06

First release. Trace format: JSON array of `{seq, tool, args, result|error}`.

### Added
- Language: `Int`/`Str`/`Bool`/`Nil`/`List[T]`/`Map[T]`/`Fn`/`Task[T]`/`Any`,
  closures, `let`, assignment, `if`/`else if`, `for … in`, `while`,
  `break`/`continue`, `return`, `and`/`or`/`not`, modules with `import … as`.
- Gradual static checker with positioned diagnostics; runtime checks at
  typed boundaries.
- Tools: `http.get`, `fs.read/write/list`, `env.get`, `time.now`,
  `rand.int`, `proc.args`, each behind a capability; `--allow`, `--seed`.
- Deterministic cooperative tasks (`spawn`/`await`), drained at exit;
  `par_map` for real parallelism.
- Trace recording (`--trace`) and verifying replay (`--replay`).
- Builtins: print, len, str, int, type, push, pop, keys, values, has, range,
  join, split, contains, upper, lower, trim, map, filter, reduce, sort,
  slice, all, assert, error, min, max, abs, json, parse_json, par_map.
- `tarn run | check | fmt | repl`; idempotent formatter.
