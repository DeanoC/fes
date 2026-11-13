# Command-snapshot physical trial

One bounded, separately authorized trial ran on TRWU8CBL against published
source `cd75ba13370232b0c11ef4b9690b5ac700d3994c`. The frozen procedure SHA-256
is `e2b0a6aefbe48a269f473c5d18bb25c65e685f3194a623e330aa725046c71883`.
It is consumed and failed; there was no automatic diagnostic retry.

The matched chain is
`78d9472a2d7218dbdfbfbc3bc8aea3ea432f789c6436937fef649ca5a28b1c20`,
boot ID `bc40ecb6064165fa`, FIT SHA-256
`5176ea7b074dd15e1414df0a39296f3b3c4579507b9a71f08e465b0584cb2486`.
The [record](result.json) preserves the transfer, failure, cleanup and rollback
observations. This is physical diagnostic evidence, not ordinary activation
or whole-board acceptance.

| Gate | Observation |
| --- | --- |
| Full input transfer | 1,708,032 bytes, two posts/two acknowledgements, complete SHA-384 binding, 11,659,771 us |
| SMMU retirement | Failed closed in phase 4, first error -3, three control writes, before queue publication |
| Command phase | 111,443 us against the unchanged 100,000 us bound |
| Source lifetime | Retained; no activation or reuse receipt |
| Exact original FIT rollback | Restored and independently read back; 17 protected readbacks before/after and 16 other files preserved |
| Warm original baseline | Booted, then DWC2 packet failure before any App command; no final Park |
| Firmware containment | Independent reader drain and bridge closure succeed |
| Host cleanup | Owned processes stopped; UART/JTAG handles closed; original USB permissions restored |

The command-snapshot optimization's reduced model read count does not establish
a physical retirement improvement. This attempt still has 134 outer ownership
guards and fails the same stage as the preceding trial. Existing command,
retirement and transfer deadlines are unchanged.

The warm baseline's packet error names USB address 1, endpoint 1, IN direction,
with one poll and no reported completion/NAK/RX. Its cause is cut off by the
collector. A later three-second read-only UART tail yields zero bytes and grants
no continuity or successful restoration evidence. Deadline expiry, device loss
and controller state remain hypotheses. The cached-cause source repair is not
physically exercised by this original-baseline firmware.

Firmware separately reports master-stop status 0, reader generation/request/ack
1 with matching cookie 1, status 4 and pending 0, followed by bridge closure
at generation 7/reset f and hardware-drained mask 8. Its zero fabric-handshake
flag is expected for this profile and does not invalidate the independent
reader acknowledgement.

The exact original FIT is
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`.
The trial ends with that FIT restored and the native baseline stopped after
panic, requiring a physical reset. Reset confirmation and baseline-only
restoration are separate gates; neither is implied by restoring file bytes.
QSPI, the factory card and the bottom kit are untouched.

The independently read-back failure archive is retained as
`de25-command-snapshot-failed-physical-20261113-r1/failed-trial.tar.gz`,
SHA-256 `45dc2a12dd713ef92cd56b7c54eef9e670b03dcd12139c7f0dba6218b2eb694d`.
The prepared source/procedure archive remains separate. No machine-local
execution approval or generated boot image is included in this publication.
