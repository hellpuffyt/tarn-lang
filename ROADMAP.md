# Roadmap

Each item is one reviewable PR.

## 0.2 — finer control

- [ ] Scoped capabilities: `--allow fs:./data,http:api.example.com` with
  path and host allow-lists enforced in the tool implementations.
- [ ] `try { } catch e { }` so workflows can recover from tool failures.
- [ ] Trace matching by `(tool, args)` so `par_map` runs replay regardless
  of completion order.
- [ ] `tarn trace show run.json` — a readable timeline of a recording.

## 0.3 — language

- [ ] Floats.
- [ ] Records (`type Item { title: Str, n: Int }`) with structural typing.
- [ ] String interpolation `"got {n} items"`.
- [ ] `match` on values and record shapes.

## 0.4 — performance and tooling

- [ ] Bytecode compiler + VM behind the same AST (5–20× on tight loops).
- [ ] LSP server: diagnostics, hover types, go-to-definition across imports.
- [ ] `tarn test` running `// expect:` files like the CI script does.

## Tools

- [ ] `llm.complete` and `llm.embed` namespaces with a pluggable backend,
  traced like everything else — the reason Tarn exists.
- [ ] `sql.query`, `s3.get/put`, `proc.run` (with the strictest capability).

## Non-goals

- General-purpose performance; Tarn coordinates work, it does not crunch it.
- A package registry. Modules are files in your repository.
