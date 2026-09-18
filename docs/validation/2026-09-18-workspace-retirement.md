# Powerboat workspace retirement — 2026-09-18

This bounded cleanup followed FES main `a362662` and operator-confirmed
full-image hardware acceptance. It supersedes the retired entries in the
[previous inventory](2026-09-17-workspace-tidying.md); it is not authority to
delete other workspaces.

## Recovery and retained data

Recovery root on Powerboat:
`/home/deano/fes-retained/workspace-cleanup-20260918.Xhh1Q1`.

The integration tree's `out/hardware` and `out/native-integration-dev` were
copied there and compared byte-for-byte with `diff -qr`. Accepted image,
diagnostic rollback files, receipts and captures are preserved outside worker
directories. Source copies remain in place too.

The integration tree at `/home/deano/fes/out/dev/library-client/fes` remains
retained, including its 28 GiB compiler cache and image source cache. Caches
were not relocated, invalidated or deleted. Docker volumes were not removed.
Canonical `/home/deano/fes` remains the starting checkout.

## Retired worktrees

All paths below are relative to `/home/deano/`:

- `fes/out/dev/fes-structure-next`
- `fes-worktrees/unified-session-spec`
- `fes/out/dev/appliance-module/fes`
- `fes/out/dev/fes-build-evidence`
- `fes/out/dev/fes-pin-fogcast-237`
- `fes/out/dev/fes-pin-fogcast-targetclient`
- `fes/out/dev/fes-ui-boundaries`
- `fes/out/dev/fogcast-core-entry-conflict`
- `fes/out/dev/fes-default-core-media/FogCast`

The first two have named top-level tar archives under the recovery root;
the remaining seven have archives under `retired/` and copies of their
administrative Git directories under `admin/`. Branches were retained.
All were clean of tracked/untracked changes. Merged ancestry was checked,
except default-core-media used aggregate patch equivalence to merged
`d5815dc`; Bob explicitly released the obsolete session-spec plan.
Ignored outputs in the seven-path batch were archived in full and checked
with `tar --compare` before removal. Their nested Git metadata had no
external worktrees; a root-visible process scan found no candidate users.
No broad workspace/output directory was erased.

Restore only into an unused exact path after checking current registry state.
Existing archives include Git pointer files; do not blindly extract them over
an active checkout. Recreate/repair registration using the retained branch and
administrative backup as appropriate, then restore ignored artifacts.

## Git metadata reconciliation

Twenty absent, unlocked worktree registrations were moved out of canonical
FogCast (12), runtime (5), and mister-packages (3) registries into
`stale-registrations/`, preserving their index, HEAD and reflogs. Their paths
were absent and Git's dry-run pruning selected those records. No relocated
pointer was found in the searched FES, Grok and temporary roots; inaccessible
temporary directories and unsearched locations remain an audit limitation.
Subsequent dry-run pruning reported no remaining stale records in those three
registries.

SG-1000's actual common Git directory is on Powerboat:
`/home/deano/fes-wt-three-pack-tip/sources/misteross/.git`.
Its `sg1000-git-restore.XoFnlb` back-pointer incorrectly omitted `/.git`.
After backing up both administrative records and the worktree pointer, a
targeted `git worktree repair` from the actual owner corrected it. Only the
obsolete duplicate canonical registration was quarantined in `sg-registry/`.
The actual SG checkout remained unchanged and clean.

## Holds and remaining work

- Retain active `misteross-sms-quartus`, its metadata ancestor above, all
  `sms-media-v2` work, and `library-client` integration/evidence.
- Retain both SG-1000 worktrees pending ownership/evidence review.
- Preserve Grok and locked Kepler workers; other teams may be active there.
- Preserve dirty/untracked trees and unproven patch-equivalence candidates.
- Mac-side Caster workspaces are out of scope and were not touched.
- A process still referenced a previously deleted Yosys working directory;
  this cleanup did not terminate it or remove further Yosys paths.

This is a safe completed first retirement batch, not a claim that every old
workspace is gone. Further retirement requires the same ownership, unique-work,
metadata-dependency and recovery checks. No kit deployment, reboot, image change
or compiler-lock unification was performed.
