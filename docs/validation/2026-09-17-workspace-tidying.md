# Powerboat workspace retirement inventory

Read-only snapshot on 2026-09-17. FES main was `095cc7a5608f502e96dba70accf2d297bcf9db5b`.
No checkout, branch, registration, artifact, cache or evidence was removed.
This is a review inventory, not permission to delete these paths.

## Method and limits

Inspected the five primary Git worktree registries (FES and its four selected
components), path existence, branch/HEAD and tracked/untracked status over
SSH to Powerboat. Inspected the extra common Git directory discovered through
the SG-1000 mismatch below. Counts cover the five primary registries, not all
clones on the machine or the Mac. Ignored outputs were not proven disposable.
The candidate list does not establish patch equivalence or owner release;
squash-merged changes cannot be classified by ancestry alone.

| Primary registry snapshot | Count |
| --- | ---: |
| Registered entries | 64 |
| Present entries | 44 |
| Missing paths marked prunable | 20 |
| FES entries alone | 22 |

The canonical `/home/deano/fes` was clean at `32f05ac38f005966df46996e80b25922c1ee2134`,
zero commits ahead and seven behind the main revision above. It was not
updated. The active integration tree at
`/home/deano/fes/out/dev/library-client/fes` is based on current main; its
documentation edits belong to this reconciliation task.

## Retain active work and evidence

- `/home/deano/fes`: canonical root and common Git metadata, with nested workers.
- Entire `/home/deano/fes/out/dev/library-client`: active FES/FogCast worktrees,
  nested metadata, `ledger/`, binaries and acceptance records.
- `/home/deano/fes-worktrees/misteross-sms-quartus`: active Caster SMS lane,
  independently confirmed by Bob in #fes. Its Git metadata lives under
  `/home/deano/fes-wt-three-pack-tip/sources/misteross/.git`; retain that
  enclosing checkout too even though its name suggests older work.
- `/home/deano/fes-worktrees/sg1000-tip-hil`,
  `/home/deano/fes-worktrees/misteross-sg1000-quartus`,
  `/home/deano/fes-tip-cold-boot-hil`, and
  `/home/deano/fes/out/dev/fes-build-evidence`: retain pending evidence archival
  and ownership review. No current bot activity does not mean no retained value.

Bob also reported two active Mac paths; they were not inspected and are not
included in Powerboat counts:

```text
ai-dev-mac:/Users/clawzai/Developer/fes-wt-sms-fes-pin
  feat/fes-sms-parent-pin at 495ff22 (reported)
ai-dev-mac:/Users/clawzai/Developer/misteross-wt-sms-quartus
```

## Dirty or untracked work: HOLD

These five non-reconciliation paths contain changes requiring an owner decision:

| Path | Observed change |
| --- | --- |
| `/home/deano/fes-tip-cold-boot-hil` | Staged `sources/FogCast` gitlink |
| `/home/deano/fes/out/dev/clean-fetch-regression` | Modified `tests/test_core_build.py` |
| `/home/deano/fes/out/dev/fes-four-slices-plan` | Eight untracked plan/spec documents |
| `/home/deano/fes/out/dev/fes-package-set/fes` | Sixteen untracked briefing/review documents under `.superpowers/sdd/2026-09-14-fes-package-set/` |
| `/tmp/fes-coleco-base` | Untracked `build` entry |

## Clean candidates: owner review, not approved removal

These 29 paths had no tracked/untracked changes observed. Ignored files,
nested repositories, branch reachability, worktree users and evidence retention
must still be checked immediately before any retirement.

```text
/home/deano/fes-clean-coleco-hip
/home/deano/fes-clean-main
/home/deano/fes-worktrees/unified-session-spec
/home/deano/fes/out/dev/appliance-module/fes
/home/deano/fes/out/dev/fes-boot-platform/fes
/home/deano/fes/out/dev/fes-pin-fogcast-237
/home/deano/fes/out/dev/fes-pin-fogcast-240
/home/deano/fes/out/dev/fes-pin-fogcast-targetclient
/home/deano/fes/out/dev/fes-readonly-core-status
/home/deano/fes/out/dev/fes-structure-next
/home/deano/fes/out/dev/fes-ui-boundaries
/home/deano/fes/out/dev/target-contract-integration/fes
/home/deano/fes/out/dev/target-service-boundary/fes
/home/deano/fes/out/dev/ui-host-consolidation/fes
/home/deano/.grok/worktrees/deano-fes/swarm/out/dev/artifact-identity/FogCast
/home/deano/.grok/worktrees/deano-fes/swarm/out/dev/image-recipe/FogCast
/home/deano/fes/out/dev/appliance-module/FogCast
/home/deano/fes/out/dev/fes-boot-platform/FogCast
/home/deano/fes/out/dev/fes-default-core-media/FogCast
/home/deano/fes/out/dev/fogcast-core-entry-conflict
/home/deano/fes/out/dev/target-contract-core/FogCast
/home/deano/fes/out/dev/target-contract-guard/FogCast
/home/deano/fes/out/dev/target-contract-integration/FogCast
/home/deano/fes/out/dev/target-contract-lease/FogCast
/home/deano/fes/out/dev/ui-host-consolidation/FogCast
/home/deano/fes/out/dev/unified-cli-session/FogCast
/home/deano/fes/out/dev/unified-kit-session/FogCast
/home/deano/fes/out/dev/unified-session-integration/FogCast
/home/deano/fes/out/dev/fes-hip-format2/misteross
```

## Missing registrations: separate metadata cleanup

Git marked these 20 absent paths prunable (`gitdir file points to non-existent
location`). Nothing was pruned; recheck for relocated worktrees before deleting
administrative records. FES and the canonical misteross registry had none.

```text
FogCast (12):
/home/deano/fes/out/dev/appliance-release/FogCast
/home/deano/fes/out/dev/appliance-release/fes/sources/FogCast
/home/deano/fes/out/dev/kit-lease/FogCast
/home/deano/fes/out/dev/linux-v4l2-preview-launcher/FogCast
/home/deano/fes/out/dev/linux-v4l2-preview/FogCast
/home/deano/fes/out/dev/pong-snes/FogCast
/home/deano/fes/out/dev/session-recovery/FogCast
/home/deano/fes/out/dev/snes-saves/FogCast
/home/deano/fes/out/dev/sofa-launcher/FogCast
/home/deano/fes/out/dev/target-discovery/FogCast
/home/deano/fes/out/dev/three-system-image/FogCast
/home/deano/fes/out/dev/zx81/FogCast

libmister-runtime (5):
/home/deano/fes/out/dev/appliance-release/fes/sources/libmister-runtime
/home/deano/fes/out/dev/pong-snes/libmister-runtime
/home/deano/fes/out/dev/snes-saves/libmister-runtime
/home/deano/fes/out/dev/sofa-launcher/libmister-runtime
/home/deano/fes/out/dev/zx81/libmister-runtime

mister-packages (3):
/home/deano/fes/out/dev/appliance-release/fes/sources/mister-packages
/home/deano/fes/out/dev/pong-snes/mister-packages
/home/deano/fes/out/dev/zx81/mister-packages
```

## Registration mismatch: do not prune by appearance

The canonical misteross registry at
`/home/deano/fes/.git/modules/sources/misteross` lists
`/home/deano/fes-worktrees/misteross-sg1000-quartus` detached at `1308f94`.
The existing worktree actually resolves to HEAD `17b69db` and common directory
`/home/deano/fes-wt-three-pack-tip/sources/misteross/.git`.
That common directory also registers the active SMS tree (observed `9c6dc96`).
The stale canonical registration was not marked prunable. Preserve both
metadata roots until explicit registration reconciliation; do not delete an
existing tree to make a registry agree.

## Safe next operations

1. Resolve the SG registry mismatch and identify all nested/common-directory
   dependencies before considering parent-directory removal.
2. Obtain owner decisions for dirty work and archive unique evidence, including
   ignored artifacts. Confirm merged patch equivalence, not just branch names.
3. Prepare an exact retirement list with retained references and recovery copies.
   Revalidate status and process/service use at execution time; never remove
   broad `out/`, a workspace root, or a metadata ancestor of a retained worker.
4. Separately coordinate a fast-forward of the canonical root and its clean
   pinned components. A checkout update is not a deployment or service start.

The [structure decision](../fes-structure.md) keeps the current repositories;
this inventory concerns disposable workspaces, not product boundaries.
