# Native Tenfoot boot selection and fixed catalog upload

The explicitly compiled `fpgaload --tenfoot-serial` path runs the full Tenfoot
App, software renderer and RunPlatform on the native CPU over ProgramBackend.
Its private catalog names programs selected in CORESEL. UI names/indexes cannot
select package paths, MMIO addresses or firmware resources. Quit during a load
waits for the outcome; during Run it requests owned Stop, then waits for Ready
or terminal containment. Preferences stay in RAM. Output explicitly declares
`serial-state-only`, without an HDMI/display grant or core keyboard forwarding.

`de25tenfootserial` selects the App loop at build time. SDK, native entry,
cold identity and composition carry the selection; prerequisite checks retain
the contained computer/rearm/SD-return contract. An unselected control build
contains neither the App loop nor its serial marker. Recovery uploads add only
`--purpose ui-catalog`, fixed to FES/UI.JSON with an 8192-byte bound.

All seven final affected checks passed, including parent Python regressions,
generated consistency and the existing native firmware C/Verilated RTL suite.
New tests run actual Tenfoot with native Go lifecycle code and modeled secure
calls/registers through serial launch, distinct relaunch, owned Quit, and launch
and return failures. Catalog/path rejection, bounded polling, cancellation,
emitter failure and copied RAM preference ownership are checked. All 35 upload
tests passed, including catalog write/readback bounds and session retirement.
The selected ARM64 SDK and entry build passed; entry.img is 7,413,760 bytes.
[result.json](result.json) binds exact source hashes, commands and artifacts.

The earlier computer project's source contract predates the width adapter and
cannot pass the current verifier. A fresh ZX81 source-bound diagnostic package
was built on the shared-lock 3090 and every exported file was verified locally.
ZX80 and the complete matched firmware chain remain separate artifact gates at
this code checkpoint. Development failures remain under
out/nano-tenfoot-serial-20261104: the obsolete project was refused, and a remote
vendor symlink was refused before Quartus ran. Corrected inputs retained the
same build limits; neither failure was a board attempt.

No hardware was modified. The physical App/core launch/return cycle, owned HDMI,
USB hub input, ordinary ROM/parts/media admission and native Ethernet remain
required before DE10-style readiness. Shared protocol/schema files and FPGA
RTL were unchanged by this feature; fresh packages use the previously committed
7808f74c2 producer source and keep diagnostic classification.
