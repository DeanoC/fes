# Warm core configuration succeeds; initial tape remains blocked

Fresh procedure `16fe97ce2214c8c8526cb2b610b7b454a4455f06bfa9ff508ca18e433e9e1978`
tests source `82e607ab`, format 24, full SOF `74383342…` and FIT `319f0380…`.
All 83 operator models, both complete native file admissions and frozen root
preflight pass. Its prerequisite is the actual `c34868fb…` original-file
restoration. Prepared archive `12ddb0a11ee43a517bb9a383f4bfdc2140de1e1f5ec2ade3e2b0de415594dc75`
verifies 10,945 logical files / 5,058 unique blobs. No attempt or approval from
the preceding consumed trial is copied.

The real trial passes fresh recovery, exact candidate FIT writing and all 17
before-and-after file readbacks. Full-SOF programming and sealed native boot
pass. Initial MENU transfers 1,708,032 bytes in two posts/two acknowledgements,
retires, continues and releases generation 4 to 10 in 925,189 microseconds.
Warm ZX81 preparation now succeeds: all 1,654,784 configuration bytes transfer
in two posts/two acknowledgements, followed by successful retirement and
continuation. The transfer result reports 14,460,224 microseconds under its
20-second bound; retirement takes 1,082,443 microseconds and continuation
reports 1,298,104 microseconds. Independent saved-wire parsing validates both
source sequences, their release/rearm lineage, owned queue cursor and clocks.
There is no preparation failure record. The earlier queue clock defect is past
this point on this exact artifact; general loading is not yet qualified.

The later native execution gate returns `instruction-fetch observation failed`
wrapping `initial tape arena unavailable, outstanding or consumed`. This does
not demonstrate a CPU fetch stall. `startExecution` calls initial tape loading
before Run. The compiled retained configuration path constructs
`NewRetainedProgramArena`, while `PublishInitialTape` deliberately rejects
`a.retained`. That guard prevents overwriting the configuration payload and
metadata still held by the firmware. A Go race-enabled overlay counterexample
completes the real retained arena, reproduces the exact tape refusal with zero
firmware calls/ordering hooks, and verifies that the entire payload/metadata
mapping remains unchanged. Production source is not edited by the overlay.
This diagnosis combines the bound compiled profile, actual error and deterministic
source reproduction; it is not a direct read of private Go heap fields.

The trial stops after 454.538 seconds, without an outer timeout, verified Running,
launch/return cycle or final USB shutdown/Park. Its 165-entry physical archive is
`618b5f4bd3fdf76acf933892a01f54ebf62acb7702d00617313a36aeaf320a41`.
The supplementary [analysis archive](analysis-retention.json) retains that actual
archive, independent two-source analysis and counterexample source/logs.
All owned processes and UART/adapter handles are independently verified closed;
USB permissions and JTAG auto/15M settings are restored. The top-kit claim stays
held. Candidate FIT `319f0380…` remains on the spare; original `d79a8e72…` is
retained on the host and has not been restored after this trial. A fresh physical
reset has been requested for restoration-only work. No reset confirmation is
assumed, and this consumed diagnostic will not be retried. QSPI, factory media
and the bottom kit remain untouched.

Next: restore the original FIT after the requested reset and separate initial
tape staging from the retained configuration source with a matched memory and
firmware contract. Preserve the retention, mapping and ownership checks; do not
remove the guard or overwrite protected source/metadata. Rebuild and qualify a
new artifact before another physical trial. Broader loading and appliance
acceptance remain open.
