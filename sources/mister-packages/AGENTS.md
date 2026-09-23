# Repository policy

Development lives only in the [FES repository](https://github.com/DeanoC/fes),
under `sources/mister-packages/`. The former standalone repository is archived.
Use a FES worktree and open PRs against FES; do not clone or update the old repository.

These rules apply to all work in this repository.

- Read `README.md`, `docs/PLAN.md`, and `docs/schema.md` before changing
  package files or the emitter.
- Documentation describes present code in present tense. Planned work is
  labelled as planned.
- A commit that changes schema, platforms, or emitted symbols updates
  `README.md` or `docs/` in the same commit.
- Git history is the archive. Do not keep deprecated copies in the active
  tree.
- Names describe current use. Do not use Overlord, numbered-stage, or
  temporary experiment labels for active files.
- This repository does not modify FogCast, libmister-runtime, misteross,
  or overlord. Consumer patches happen in those repos after the oracle
  matches.
- Package YAML is the source of truth. libmister-runtime keeps generated
  C++14 headers as reviewed target text for the ARMv7 Linux HPS. Change
  `testdata/oracles/` in the same commit as any intentional constant
  change, and record the runtime commit in the oracle file. Regenerate
  the runtime headers in that consumer repository; do not hand-edit
  them. Do not compile the runtime with a development-host toolchain.
- Do not add a connection solver, Verilog generator, SVD emitter, template
  CLI, or package-manager clone step without a demonstrated need and
  explicit approval. `core_source` files are pins, not clone recipes.
- Do not call this tree a catalog.
- Tests are proportional: parse, validate, and oracle-diff the real
  platform and system YAML. Do not invent a second SoC or a second
  system to prove the loader.
- Commit, push, and open pull requests as needed for the requested work. Merge
  a pull request only with user authorization.
