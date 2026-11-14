# DE25 owned-cache startup and stopped return observer

The single bounded TRWU8CBL procedure stopped at the unchanged 45-second
transfer/read/return deadline. The saved capture, which includes the runner's
built-in two-second passive tail, independently replays twelve full-frame
CRCs, local drains and the final secure firmware return. This late complete
capture does not upgrade the deadline result. No retry was made.

Native source is `02ece2cd57c2ae0e4c2cdd6ced8809c5939a32d0`. The unchanged FPGA producer
is `df234b524d0e1340b343df411c841bd1dc553817`. The complete Tenfoot command handler remains
linked: ELF 14,921,903 bytes, entry 7,303,168
bytes and FIT 7,847,928 bytes, below the existing 8 MiB bound.
The normal native UI loop was not invoked.

Temporary EL2 caching covers only owned Go RAM and materialized loader pages,
with a single device UART page. Go RAM is execute-never at EL2. The same C
planner produces the host and runtime tables, checked against a layout-bound
CRC. Live control and translation checks precede decoding. Register-only exit
cleans owned ranges and restores the original control registers before the
existing clean EL1 handoff. Device ownership bounds are unchanged.

| Inclusive CPU interval | Seconds |
| --- | ---: |
| map_prepare_us | 0.069248 |
| inflate_us | 1.154755 |
| checksum_us | 0.018463 |
| elf_plan_copy_zero_us | 0.015696 |
| cache_clean_restore_us | 0.044533 |
| loader_to_handoff_us | 1.311517 |
| handoff_to_main_us | 0.049755 |
| retain_us | 55.328254 |
| loader_to_retained_us | 56.789923 |

All sixteen ordered startup checkpoints pass. The prior stopped source
`b1965e3e8` spent 62.316562 seconds inflating and 63.963775 seconds before
handoff; this capture measures 1.154755 and
1.311517 seconds respectively. These are one sample
per source, include trace output and are not a general performance signoff.
The retained-package observation took 57.300756
seconds within its unchanged 100-second stage.

The final saved trace contains twelve changing 3,686,400-byte frames. Each
complete FPGA CRC matches its CPU publication and each local drain precedes
buffer reuse. Final firmware records reader/request/ack generation 13,
cookie 13, status 4 and pending 0; closure generation 7, reset mask 0xf,
hardware drain mask 8 and producers quiescent. Optional fabric-warning
acknowledgement remains unqualified. Final closure is observed in this
capture, while the overall procedure remains stopped at its deadline.

Offline replay reproduces the saved decoded trace, rejects twenty damaged
frame/ownership/closure captures and rejects each of sixteen missing startup
checkpoints. No later hardware observation or command was used. Six affected
checks, twenty focused map/bounds tests, fourteen actual ARM QEMU cases and
the full model launch/return/failure path passed before the hardware attempt.
The retained model ELF has older VCS metadata; all selected source bytes
match this commit, and entry/firmware/SDK were rebuilt on this commit.

Host audit confirms the original 15M JTAG clock, exact USB permissions,
stopped private server and free UART. The exclusive claim and spare card
remain held; the factory card stays safe outside. No factory restoration was
performed, as requested. The frame arena remains pinned until reboot.
The six prior failed procedures and the separate smaller-image physical
pass remain retained. Ordinary core admission, HDMI, USB hub input and native
networking remain open; the full goal is active.

The [result](result.json), [raw capture](native-console.log),
[CPU checkpoints](startup.json) and [offline replay](replay.json) preserve
these separate evidence boundaries. The stopped matched-chain archive is
`/home/deano/fes-retained/de25-entry-cached-20261104-stopped/stopped-native-chain.tar.gz` with SHA-256
`81986d9429b0636c1d164fd3664e2b709e55d157ce30bf9fea2b3b0b74962af7`. All 45,988 members
were read back against its manifest. The standalone source bundle restores
without alternates and passes strict fsck. Expanded output files remain.
