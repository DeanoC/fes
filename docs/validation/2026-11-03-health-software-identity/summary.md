# 2026-11-03: agent health without the software receipts

This is a Kit A check of the change for #1438. Agent health now reports the software identity instead of the supervisor's whole status. Evidence class: hardware diagnostic on Kit A, not acceptance.

## Method

[`k1438-run.py`](k1438-run.py) claimed Kit A and measured two 120 s idle windows.

1. **Baseline:** the first window ran on baseline software `89173748…`.
2. **Candidate:** the driver then installed and activated scratch bundle `a9659b6f…`, built from `4ae587093` on image `f904961e…`, with no image change and no reboot. After a 30 s settle, the second window ran.

Each window recorded the same things:

- **Kit CPU:** [`k863-idlecpu.sh`](k863-idlecpu.sh) read per-process CPU on the kit.
- **Host traffic:** the driver read the shared Powerboat host's `ss` byte counters on its connections to the kit agent. It only read counters; the host was not touched.
- **Health size:** one authenticated and one unauthenticated `GET /v1/health`.

Afterwards Kit A went back to `89173748…`, was verified, and was left idle and lease-free. [`h1-results.json`](h1-results.json) holds the record.

## Results

| | Baseline `89173748` | #1438 branch `a9659b6f` |
| --- | --- | --- |
| Authenticated `/v1/health` | 210,210 B, 691 receipts | 993 B, four identity fields |
| Host receives from the agent | 594,464 B/s | 4,035 B/s |
| Host sends to the agent | 1,250 B/s | 1,269 B/s |
| `mister-agent` CPU | 31% of a core | 18% of a core |
| `fes-kit-software` CPU | 46% of a core | 47% of a core |
| System busy | 42% of 2 cores | 36% of 2 cores |

## Findings

1. **The traffic is gone.** The host's request rate was the same in both windows, but each response shrank from 210 KB to under 1 KB.
2. **Agent CPU fell by 13 points.** The rest of the agent's idle CPU is still the supervisor round trip it makes per health call, plus its other work.
3. **Supervisor CPU did not change.** The supervisor still reads, validates and encodes its whole state file, receipts included, for every health call. That half needs an image change and is tracked separately.
