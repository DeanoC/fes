# Atari ST startup budget and recovery validation

PR #569 was rebased onto `c84bcd8d8a20b5cf2746cc3592ce45999b5c4578` (including #567). The selected software build is `432fec4d0d82e1a838786d9c38635d5a581288ce`; subsequent documentation commits do not change its executable sources.

Every format-3 ROM-linked `fes.atari-st` library activation now gets `max(upload_timeout, 300 seconds)`, including an empty drive, a selected disk, cartridge and video-part combinations. Caller cancellation and earlier caller deadlines still apply. Other cores retain their configured budget. Main at the selected base also used the generic 60-second budget for ST; the reported approximately 77-second pre-PR launch would exceed it. Empty-drive GEM boot remains supported.

Failed or ambiguous CPU release now saves a bound disk before idle cleanup. A failed save retains recovery ownership and staged inputs. Explicit `reboot_required` responses likewise retain staging until a confirmed clean Stop. An active load reply must still own its dispatch grant, including its local expiry; reconciliation cannot turn a lost lease into launch success. Lease TTL and heartbeat cadence are unchanged.

ROM staging borrows immutable operation-owned bytes, reuses the sealed map within that operation, checks cancellation and avoids oversized read/copy growth. Private publication and restart adoption retain independent validation. Host profiling of the same envelope produced the same programmed RBF hash in all 12 runs. Median total allocations fell from 277.2 to 110.3 MB on amd64 and 261.9 to 102.6 MB on 386. These are host measurements, not ARM measurements or proof of the cause of a target renewal failure.

## Checks

The [machine-readable evidence](2026-10-06-atari-st-startup-budget/evidence.json) records exact package, ROM, disk, software and capture hashes, launch durations, byte comparisons and cleanup. Tests include the ST budget matrix, real non-ST budget, failed-release/save recovery, explicit recovery staging, lost/expired grant replies, cancellation and envelope framing. The affected Go packages passed race tests. The final targetclient suite was repeated after the expiry guard; the other affected package sources were unchanged. The full native runtime suite passed on the same native source tree. Parent consistency checks and exact host, CLI, ARM runtime and agent builds passed. GPT-6-sol reviewed the selected software with no actionable blockers.

## Hardware scope

Kit A runs use a temporary diagnostic runtime/agent overlay and the unchanged frozen ST package produced at `f4551a4d58dc1deadb8a979c77609b37bdd720cf`. This is exact-package hardware validation, not acceptance of an assembled appliance image. The host configuration remains `upload_timeout_seconds = 60`. Video is built-in Direct; physical cartridge, expansion and alternate video output are outside this hardware check.

All three final boots passed using normal library Play without guest input:

| Mode | Launch time | Result |
| --- | ---: | --- |
| Diskless | 47.551 s | GEM; drive A empty and volatile; normal Stop |
| Fresh disk | 55.043 s | AUTO completed; Save and Stop passed; all 737,280 bytes exactly match the seeded model |
| Saved disk | 56.346 s | Cold private-host restart; exact saved revision bound before release; prior markers verified before writing; Save and Stop passed; all 737,280 bytes exactly match the restored model |

The payload hashes are `a9d2a28156ffbef7989f42d99272b844eb1af519754e4486e72bd550fc99b53c` and `c5a68818e6ac53d11e1c2f3596e61b0ad55eb7b11924fa79c3cb65d9e9a1744b`. No timestamp variation was needed to obtain either exact match. Runtime and agent process lifetimes remained unchanged across the probes.

Earlier testing at `a70164ed0` included a pre-programming `INVALID_ARCHIVE` rejection after 247.7 seconds and a restore reply that reported active after the last renewal's TTL had elapsed. That restore subsequently showed black HDMI and idle, so it is a failed acceptance attempt. The renewal failure's underlying cause remains unestablished. These observations prompted the grant/expiry guards and staging allocation reduction. They are retained separately from the final runs; they are not evidence of a nextpnr defect.

Final cleanup removed the owned overlay and private host/config, verified the original factory hashes and unchanged boot, restored the normal host and autostart, confirmed all 4,253 library games, and inspected the populated menu with the target idle and lease free. The earlier [AUTO boot record](2026-10-06-atari-st-auto-boot.md) remains historical evidence for its own software revision.
