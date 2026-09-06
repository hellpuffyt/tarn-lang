# Changelog

Format: [Keep a Changelog](https://keepachangelog.com). Versions: SemVer.
Language and trace-format changes are called out explicitly.

## [Unreleased]

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
