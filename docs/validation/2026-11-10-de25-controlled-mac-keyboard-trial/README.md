# Refreshed offline trial for controlled MAC startup image

The launch/return bundle now selects native boot ID `a02d808ea11eb54f` from the
[controlled startup boot chain](../2026-11-10-de25-mac-startup/README.md).
Its compiled implementation is `db5fc2d66b1426e79e63a77577499d1139eda33e`;
preparation starts at documentation checkpoint `65c5193f1`. This step changes
only integration records and private test files. No runtime, firmware, shared
contract or FPGA source changes are made.

The fresh bundle is `out/nano-controlled-mac-keyboard-trial-20261110/`. Its
13-purpose upload plan binds the current SDK/composition receipts and the new
FIT (`daa6549d2ebf88a0c653f8257191611c40b083e99b525ee490bb30ec5cb2905e`).
The corresponding HPS-first SOF is
`2cf4be71c5d474df6ef8f32af4e3fc565fb4f39665d6df5a6a8e9ea4d7aec0c6`.
The chain's composition checks 26 direct input hashes and rechecks 2276 native
inputs. The original FIT/memory bounds and sealed FPGA payload stay intact.

Only helper source, scan-probe bytes, tape proof data and 12 immutable program
assets are copied from the previous offline bundle. All source/proof/asset
copies have recorded hashes; helper paths and new artifact bindings are adapted
in the fresh directory. No old live preflight, programming grant, consumed
reservation, upload receipt, private JTAG configuration or console is copied.
The previous bundle and failed physical trial remain preserved for their own
sources and images. Package admission identities are file provenance and grant
no live programming permission.

All 45 staged Python model/boundary checks and the one race-enabled Go
real-package preflight pass. They exercise full menu/core/return sequencing,
input and step receipts, MMU proof forwarding, programming/clock cleanup and
terminal failure handling. The package check uses this newly built SDK and the
real sealed ZX81/ZX80 selections, retained ROMs and 917-byte ZX81 tape, with zero
firmware calls. A negative upload test supplies the previous existing FIT with
its correct hash/size: it is still rejected as the wrong selection. A wrong
current hash and missing preflight also reject before device actions. Historical
traces used by failure decoders remain historical evidence.

After this documentation checkpoint, `source.json` is refreshed to the exact
checkout and `prepare_offline.py final` performs the full file-only native,
recovery, sealed-tree, source, helper and asset validation once. It creates
`native-file-admission-final.json`, checks fresh closure hashes, and rejects a
wrong admission digest and lost ownership predicate. The replay predicate is
explicitly for file validation; it claims no physical lease. The final receipt
is `admission-proof-final.json`, and `offline-preparation.json` retains the
completed check/log/input inventory. Preparation counts as passed only when
those final records exist and pass. This avoids repeating the full validation
solely because the documentation commit updates the checkout identity.

No tracked operator-source record, live `trial/preflight.json`, lease refresh,
probe execution, USB inventory or hardware session is created. The user is away
and has deferred physical checks; a calendar change does not resume them.
Old-output cleanup remains paused. The generic Nano network opener still refuses
startup; the new controlled hook is not selected and the secure MAC provider
remains unbound. Ethernet is disabled, SD read-only and preferences volatile.

When the user returns, revalidate the continuing TRWU8CBL-only lease, exact
unique stable USB connection, free UART, current source and candidate hashes,
and keyboard/monitor availability. Freeze one new bounded operator attempt.
The other kit is excluded; preserve the original factory card. Factory restore
remains waived by the user. No current board state is inferred from host files.

The planned cycle is menu -> ZX81 plus tape -> physical keyboard Stop -> menu
-> ZX80 -> owned Quit -> menu and final Park. Signal for one Ctrl+Alt+Escape
press/release only after the first core is running. The keyboard return requires
fresh USB report progress with an empty UART payload. Visible menu/core output
requires a separate human HDMI observation. Expected logical generation is 5,
with three menu sessions, five configurations and firmware closure generation
29/reset mask 15.

Existing bounds stay: human wait 60 seconds, core close 5, menu restore 100,
initial Ready 100, native 450, outer trial 800 and host 890. First failure ends
the attempt, with a two-second passive tail and no retry/grant replay. Cleanup
restores 15 MHz/automatic JTAG settings, exact USB permissions and releases only
the owned UART/server. Host detachment does not establish FPGA containment.

This image has no physical boot, keyboard return, HDMI, Ethernet or ordinary
core-load acceptance from this preparation. A source-bound qualified MAC/clock/
PHY/SMMU supplier and independent drain, boot selection of its controlled hook,
writable SD, broader packages and DE10-equivalent appliance acceptance remain
open. The overall goal is incomplete.
