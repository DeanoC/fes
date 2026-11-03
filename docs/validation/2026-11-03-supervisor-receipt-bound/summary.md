# 2026-11-03: kit software supervisor with a bounded receipt ledger

This is a Kit A check of #1444's supervisor change. Evidence class: hardware diagnostic on Kit A, not acceptance.

## Method

Deano approved a runtime bind-mount for this check. [`k1444-run.py`](k1444-run.py) claimed Kit A and ran these steps:

1. **Back up the state.** It copied `state.json` (693 receipts, 209,892 B) to Powerboat. The backup is not committed, because it holds requester names and operation IDs.
2. **Baseline window.** It measured a 120 s idle window on the image's own supervisor, `901a51bf…`. That supervisor builds byte-identical from this branch's `origin/main` merge base, so it is the control.
3. **Swap.** It stopped the software group with the init script. It bind-mounted a tmpfs copy of this branch's static ARM build, `da2627a8…` built from `f8917512f`, over `/usr/sbin/fes-kit-software`, and started the group again.
4. **Candidate window.** After a 30 s settle it measured a second 120 s window.
5. **Restore.** It stopped the group, unmounted the binary, started the image supervisor again, and verified the running binary's hash, the absence of any bind mount, the baseline bundle `89173748…`, an idle runtime and a free lease.

Both windows measured the same things:

- **Kit CPU:** per-process CPU on the kit, from [`k863-idlecpu.sh`](k863-idlecpu.sh).
- **Host traffic:** the shared Powerboat host's `ss` byte counters on its connections to the kit agent. They were only read; the host was not touched.
- **Response sizes:** the authenticated `/v1/health` and `/v1/kit/software`.

The baseline agent was running in both windows, so health still embedded the full software status. #1445 is not part of this check.

The new supervisor pruned the ledger when it started, and Deano chose to leave Kit A pruned. The image supervisor reads the pruned ledger. [`s1-results.json`](s1-results.json) holds the record.

## Results

| | Image supervisor `901a51bf` | Branch supervisor `da2627a8` |
| --- | --- | --- |
| Receipts in `state.json` | 693 | 64 |
| `/v1/kit/software` | 210,059 B | 19,575 B |
| Authenticated `/v1/health` | 210,802 B | 20,318 B |
| Host requests (bytes sent to the agent) | 1,249 B/s | 1,327 B/s |
| Host receives from the agent | 597,891 B/s | 61,505 B/s |
| `fes-kit-software` CPU | 47% of a core | 6% of a core |
| `mister-agent` CPU | 31% of a core | 12% of a core |
| System busy | 42% of 2 cores | 12% of 2 cores |

## Findings

1. **The supervisor's idle CPU is almost gone.** The host's request rate was about the same in both windows; the candidate window had 6% more request bytes. Each status read now handles 64 receipts instead of 693.
2. **The agent and the network gain too, even without #1445,** because the status the agent embeds in health is 10× smaller. With #1445 as well, health drops below 1 KB. That combination was not measured.
3. **Kit A kept 64 receipts.** Every receipt on the kit was one accepted activation per generation, none were at the current generation, and the unreleased completion fell within the 64 newest generations.
4. **A second unbounded store, not addressed here:** `control/` holds 732 `admission.key.grant-*` files. They are read by ID, not per status request.
