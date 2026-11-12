# DE25 runtime programmer diagnostic, 2026-10-31

The actual internal `LinuxDe25FpgaManager` programmed the known BRAM ZX81
image twice in one context on bottom board TRWU8J8G. A fresh context then
refused the inherited overlay before submission. Native control loaded and ran
BASIC; shared HDMI captures show the ROM prompt, LOAD token, loaded state and
`7`. This is diagnostic evidence, not production package or normal-kit HIL.

## Selected bytes

- Runtime C++ producer: `1a3e2b86086733d39b0a5311600b1f3ec9c6e098`.
  Production `src/` and `include/` bytes are unchanged at final runtime-suite
  producer `5d2eb6220a895579962168159e1797d3a9b729db`; that commit corrects
  the exact archive guard and its missing-object fixture, plus README prose.
- Static harness SHA256: `5003019ada7a418e41c67e30ef8cf472881ef6a5814cf5db47132a38bbeaa13c`.
  It links the actual runtime library; it is an explicit test harness, not an
  installed daemon entry point. Other SDK output identities are in sdk-build.json.
- Running kernel producer: `29e0eca19228a7b3027e5d7729202618a6bf79a9`;
  Image SHA256 `a4604214a70855ec824ffbf70c45abbd82983dcbefd68bb9f51de7b7089ab478`.
  Running notes SHA256 `be78380775fe921a08e9b5f1ab312df073338290fe0ebe06777a21b075462b9b`.
- Known non-DMA RBF SHA256: `7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a`.
- Matched receipt/image producer: `14ea3760b016825d96aeacc3aeca8f22dc04000d`.
  Two identical offline assemblies: image-verify.json. Only the root kernel
  receipt was updated live; no whole-image flash or new boot occurred here.
  Receipt SHA256 `8baeadd914a41067d9210719280994278d42f27548a54d850d366f7dfabf2370`.
- Boot `15e107d8-0ed1-4e5e-83d8-b27c8a49d853`, DHCP 192.168.10.28,
  SD CID `275048534436344760702d33e0017c00` remained unchanged.

## Results and retained failures

The final full runtime suite passes at 5d2eb6220 in 256.4 seconds, including
functional, incremental, version, exact archive and hardware-support-truth checks.
The locked AArch64 SDK builds the archive, daemon and harness and passes QEMU
filesystem tests at 1a3e2b860 (15.8 seconds). The image assembler passes five
focused cases. The generated FDT was independently decoded using the pinned
kernel DTC (programming-decoded.dts).

The earlier affected run at 14ea3760b passed seven of eight commands, including
host Go/UI/browser, but the runtime archive guard rejected diagnostic wording.
The runtime retry at 1a3e2b860 passed functional tests and failed because the
exact archive manifest omitted the new object. Both failures are retained;
5d2eb6220 fixes the manifest and adds a missing-member negative fixture.
There is no claim of one all-eight affected run at the final head.

Final physical receipts are under board/. program-two-cycles passes twice;
fresh-context-refused returns expected status 8 with attempted=0. Seven native
operations client-002 through client-008 pass. client-001 returned126 before
execution because an uploaded helper lacked its execute bit; chmod corrected
that test fixture. No FPGA failure is inferred from that permission error.

![BASIC 7](board/seven.png)

Initial 14ea3760b diagnostics also programmed twice and displayed7, but are not
used to qualify the final C++ source. Four overlay-property lifetime warnings
occurred per two-load trial (eight total in the unchanged boot); cleanup.stdout
preserves them. No physical failed transfer, DMA grant or reset injection ran.

Final guest HOLD and explicit known-success cleanup pass: owned overlay,
configfs mount, firmware and temporary clients are absent. Owner root:root600
and actual unconfigured userspace syscalls pass afterward. All owned clients
are closed. FPGA SRAM is not attested erased. Candidate claim remains retained;
there is no baseline promotion or release-as-restored, and no QSPI/JTAG/card write.

## Remaining normal-kit gates

The package profile table and production AArch64 factory remain closed. Safe
restart adoption, actual DE25 CoreDriver integration into the one lifecycle,
agent/catalog/MENU and normal launch/stop/relaunch remain unfinished. Software
deadlines cannot interrupt configfs release, a secure call or shared-service
mutex. Physical failed-transfer retention, global DMA drain/reset, DDR/SMMU/
firewall qualification and timing issue #1284 remain open.

Independent artifacts, including full SDK/image and failed-run logs, are
retained privately at `/home/deano/fes/out/artifacts/de25-runtime-adapter/1a3e2b860/`.
Public receipts are indexed by receipts.sha256.json; no authentication helpers
or password files are published.
