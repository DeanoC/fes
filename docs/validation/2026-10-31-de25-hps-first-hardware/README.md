# DE25 HPS-first Linux loading and ZX81 hardware proof

The prepared HPS-first bootstrap boots the approved FES SD, then Linux loads
ZX81 without a preceding full-core JTAG configuration. The physical AArch64
maintenance client loads and runs the saved BASIC program; HDMI shows `7` and
`0/1`. This is a physical diagnostic on bottom board TRWU8J8G, not managed-kit
or production-runtime admission.

The FES integration scope contains evidence and documentation only, based on
`3af76f1273d7fa0b59149e2ef46b142dec973626` (platform branch including #1302).
No RTL, runtime implementation, kernel, shared definitions or clock constraints
change. The vendor kernel remains `6.12.11-gd7d192a9ddd9`; the #1300 recovery
kernel is not installed.

## Results

| Starting configuration | Linux core load | Native control / HDMI | Reboot or recovery |
| --- | --- | --- | --- |
| Factory FPGA/QSPI, FES SD | Fails: buffer timeout, manager remains `write`, cleanup stuck in `kthread_stop` | No CPU fabric access after failure | Clean Power down observed; next verified hardware epoch uses volatile HPS-only bootstrap |
| Complete matching HPS-first SOF via JTAG | Two loads pass in one Linux boot, manager `operating`, overlay `applied` | Both BASIC LOAD/RUN sequences show `7`, `0/1` | Ordinary reboot stops after `Restarting system`; reloading identical SOF recovers Linux |
| Matching HPS-only RBF via JTAG, no core included | Linux boots first; subsequent core load passes in same boot | Native reset, tape upload, LOAD/RUN show `7`, `0/1` | Clean shutdown and requested factory-baseline restoration |

The factory-start epoch reports SDM 25.1.1; the new bootstrap contains 26.3
firmware and uses HPS-first initialization. This comparison does not isolate
firmware alone: configuration order and static configuration differ too.
It does establish that leaving the factory startup unchanged has not passed
this core-load test. The failed stock-driver cleanup remains relevant to #1298;
this evidence does not close that issue.

The two loads after the complete SOF preserve boot UUID
`c9344a6c-5fbf-4ea8-afc8-7cc937bcdee0`. The HPS-only bootstrap boots UUID
`35618839-7f3d-4d5c-94ae-51c3118750aa`; Linux loads the core without changing
that UUID. Pinned SSH and Ethernet remain responsive. Native clients operate
direct CPU MMIO at `0x20010300`, span32, rather than a host proxy. No JTAG
transactions occur during any Linux load or native-client operation. After the
first complete-SOF Linux load, a separately closed JTAG service checks only
endpoint ID, version and fault before native control begins.

The immediate ROM capture after HPS-only reset is blank; later LOAD completes
with `0/0` and RUN with `7`, `0/1`. After the second complete-SOF Linux load,
RUN alone also gives a blank picture; explicit HOLD-reset/RUN restores the K
prompt and permits LOAD/RUN. Preserve explicit post-load reset sequencing and
observe ROM readiness before input; a control acknowledgement alone is not a
picture-readiness guarantee. All blank captures are retained.

![HPS-only startup followed by Linux core load and BASIC RUN](hps-only-run.png)

## Artifact and execution identity

[artifacts.json](artifacts.json) preserves the original off-board packaging
receipt (its unqualified/not-performed fields describe packaging time);
[observations.json](observations.json) records this later hardware execution.
The compiled HPS-first producer is
`79d616e85e19d65f0d645b67a85ffe7e142dcdfc`, complete SOF SHA256
`0a9475308c4411664083377e7865ad7dedac287bc3b8c4b6fcb520386c7fcc5f`,
and core RBF SHA256
`7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a`.
Computer build ID is `06ee047f143317df0fd1d98717b2b991`.

The later HPS-only packaging is a diagnostic derivative of that exact complete
SOF, not a new compilation. [hps-only-artifacts.json](hps-only-artifacts.json)
records its recipe and hashes: Quartus26.3 Build114 `quartus_pfg -c
linux-complete.sof boot.rbf -o hps=on` produces `boot.hps.rbf` and an identical
core RBF. PFG inspection confirms HPS_FIRST, CMF26.3, HPIO and HPS sections.
Only the `.hps.rbf` is sent with `quartus_pgm -m JTAG -o
'p;boot.hps.rbf@1'` on exact cable `DE25-Nano [3-11.3-iface0]`.
Programming succeeds with zero errors/warnings. No flash-loader initialization,
JIC programming, erase, force, QSPI, SD or boot-configuration write occurs.

The frozen AArch64 client SHA256 is
`13d8fd22d7f16e4c93f6f4adcb7120da555d966752f80f31782808af6f1a3539`,
from source `24fc445a2c68a8cb8945bf852bc9c61846b06b01`. Its command receipts
are [native-results.json](native-results.json) and
[hps-only-native-results.json](hps-only-native-results.json). The saved tape
fixture SHA256 is
`b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71`.
Seven selected baseline file hashes pass, separately from writable-card identity;
this is not a full SD reattestation. Actual printed FPGA OPN suffix remains
hidden under the fan, so acceptance is limited to this board and artifacts.

## Retained evidence and remaining gates

[observations.json](observations.json) summarizes epochs and outcomes.
Programmer logs, manager states, failed cleanup stack, native receipts and
unmodified HDMI frames are retained alongside compressed UART logs. Published
UART/log text redacts private IPv4 and MAC addresses; raw captures and SSH
credentials remain private. All captures use the shared
`/dev/v4l/by-id/fes-ms2109-hil` feed; its producer and other viewers are untouched.

Before each volatile bootstrap, graceful Power down is observed. After tests,
the core is held, owned overlays unmounted, firmware search override cleared,
and uploaded clients/media/modules/bitstreams removed with absence checks.
After a failed transfer, modules and uncertain service buffers are not unloaded
or reused before a verified hardware reset. Known vendor overlay removal leak
warnings are preserved. USB permissions return to root:root0664; no task UART,
SSH, JTAG master or capture session stays active. Final restoration status is
recorded in observations and the board's authoritative Powerboat status log.

Next integration is reproducible bootstrap/kernel-module packaging and bounded
runtime loading/reset/stop, followed by managed AArch64 software and MENU/DMA.
Persistent boot-flash programming still requires explicit authorization and a
concrete recovery procedure. JTAG bootstrap success does not prove QSPI boot,
ordinary reboot recovery, failed-load DMA quiescence, production package
admission, PR personas, MENU, discovery/catalog launch or DE10 workflow parity.
