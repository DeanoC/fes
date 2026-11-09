# Physical keyboard return trial

Native image `ff0e7da217a21406`, compiled from `545f34022`, adds the deliberate
Ctrl+Alt+Escape return through the real Tenfoot App Stop path. The sealed menu,
ZX81 and ZX80 packages retain their tested FPGA identities. The complete matched
SDK/entry/BL31/SPL/FIT/SOF chain and software checks pass. This image has not yet
been programmed. Only top TRWU8CBL and its installed spare card are selected.

The bounded test performs five configurations: menu → ZX81 (+917-byte tape) →
menu → ZX80 → menu, then final Park. The first return requires the selected USB
keyboard Stop at logical generation1 and positive report progress, with an empty
UART payload. The separate human-action wait is at most60 seconds after the
explicit live signal. Core close still has5 seconds and menu restoration100;
native450/outer800/host890/initialReady100 remain unchanged. Expected final
service generation is5; firmware bridge closure is generation29/reset mask15.
A second launch and final owned Quit follow. No image or grant is replayed.

The operator reports that a keyboard and monitor are available. Visible output
needs a separate operator observation; serial frame receipts alone do not prove
it. The first failure ends the trial with a two-second passive tail. Independent
cleanup must restore15MHz/auto-adjust, exact USB permissions and release the
owned programming server/UART. Factory restoration remains waived; the original
card stays safe. Old-file cleanup stays paused. Broader input, native networking,
whole-board timing and ordinary appliance acceptance remain open.
