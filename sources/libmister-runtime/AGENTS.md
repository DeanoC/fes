# Repository policy

Development lives only in the [FES repository](https://github.com/DeanoC/fes),
under `sources/libmister-runtime/`. The former standalone repository is archived.
Use a FES worktree and open PRs against FES; do not clone or update the old repository.

These rules apply to all work in this repository.

- Read `README.md` and `ARCHITECTURE.md` before changing execution behavior.
- Update documentation and the [support matrix](docs/support-matrix.md) in the
  same commit as the behavior or support status they describe.
- Maintain one active lifecycle, one daemon, one profile table, and one Linux
  production construction path.
- Delete superseded implementations from the active tree; use Git history
  instead of keeping deprecated copies.
- Do not introduce active POC, numbered-stage, or temporary names.
- Do not add an ownership database, authentication layer, security framework,
  failover system, retry framework, or recovery coordinator without a
  demonstrated current need and explicit approval.
- Do not claim hardware support or hardware-working behavior without dated
  physical-Pi evidence that identifies the runtime commit and image.
- FPGA, input, video, save, and lifecycle behavior changes require real
  hardware tests before their support status can become hardware-tested.
- Preserve unrelated user work and do not overwrite or discard it.
- Commit, push, and open pull requests as needed for the requested work. Merge
  a pull request only with user authorization.
