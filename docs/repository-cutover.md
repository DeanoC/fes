# Publishing the consolidated repository

This branch contains first-party source under `sources/` as tracked modules.
The local import preserves original histories and records the mapping in
`config/source-imports.toml`. External toolchain and upstream source locks remain
independent. No old remote repository is deleted or automatically archived.

Before publication, repeat `make source-status` and compare the former component
remote heads against the import mapping. Reconcile any merged or active team
work explicitly. A read-only remote query cannot discover unpublished branches.
Announce the short cutover in the existing team channel and poll any replies.

Publish this migration with a **merge commit, not squash or rebase merging**.
All imported component commits listed in `required_parents` must remain ancestors
of the resulting FES main. Keeping their SHAs in a TOML file alone does not retain
their history. The historical FPGA build verifier needs the original Git objects.
Verify ancestry and a fresh clone after the merge before retiring old development
entry points. The import mapping identifies the exact source trees to compare.

For existing developers, preserve dirty component worktrees in place and clone
the consolidated FES into a new directory. Port pending diffs into one FES
feature worktree, then validate them. Do not delete or reset the old nested
repositories as an upgrade shortcut. Follow [development](development.md) for
the new commands and cache locations.

Use the always-reported `integration` result as the required aggregate check and
require testing against current main. The workflow handles `merge_group`; enable
a merge queue only if the repository supports it. Otherwise one integrator
serializes merges and retests candidates after base changes. The observed remote
currently has no branch protection or active rulesets; this branch does not
claim to have configured them.

Retain former repository URLs for history and PR links. Once all teams have
moved and fresh-clone/build checks pass, mark those repositories as historical
and direct new development to FES. Do not maintain two writable authoritative
main branches or continue internal pin-update PRs.

Source publication does not deploy an image. Promote only exact verified
artifacts, preserving separate software, compiler and hardware evidence.
