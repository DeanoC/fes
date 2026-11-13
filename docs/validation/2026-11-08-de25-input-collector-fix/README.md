# Typed input diagnostics in native collectors

Before the first hardware attempt on boot69a03065693442b5, inspection found
that the new Go `FES_NATIVE_INPUT_ACTION` records entered the collectors'
firmware accumulator. That would misidentify the first firmware mutation and
fail USB pause ordering or quiet-terminal checks. No programming, SD write,
clock change or device mutation had begun. The169a10634 procedure is superseded
while its native image remains unused0/max1.

Implementationb064df41e5dcba2820e0eddea3391198b67ad6b0 decodes the records
separately in both serial and paired collectors. Each launch/Stop requires its
UART Select/Stop record. Quit requires Stop under the running generation and
Quit under the returned generation; paired Quit follows verified menu
restoration. Counts, source, owner, state, generation and ordering are checked.
Startup or unsolicited USB lifecycle input rejects. Missing, duplicate,
malformed and reordered records retire the client before another command.
No broad quiet-line exception was added: all original boot/configuration/fetch,
USB pause/resume/Stop, reader/master drain and owned-return evidence remains
mandatory. Input diagnostics supply none of that authority. Historical UART
fixtures are unchanged and are not upgraded by inserting observations.

A test-only Go overlay executes the actual observer with injected service
status and zero firmware calls. Its seven exported lines are explicitly host
data. The producer/collector test re-exports and compares those bytes, runs both
collectors through complete launch/return cycles and checks missing/foreign/
malformed/reordered actions and missing firmware/drain evidence. Synthetic
models use the captured formatter with their explicitly synthetic generation;
no model row is physical UART evidence. Nine join tests and42 existing observer
tests pass. The complete parent suite passes1,733 tests with three existing
skips; all six selected parent/consistency checks pass. Current staged startup,
collector, receipt, tape and19 lifecycle fixtures pass. Nine additional receipt
mutations reject. Initial host test fixture/assertion errors remain retained
alongside their corrections.

All2,260 native producer/source/artifact digests still match the compiled930775b1d
image. No SDK, entry, BL31, U-Boot, FPGA payload or composition was rebuilt.
Full native-file admission now includes the collector modules and Go fixture;
5,530 files/eight trees pass, with fresh worker hashing and six negative-prefix
refusals. Cached admission still grants no live authority.

The existing experiment/progress and recovery/native scan fuse paths remain.
The same image has one total allowed attempt, with zero consumed; this host
correction adds no allowance and replays no image. All original hardware bounds,
clock/permission/private-server/UART cleanup and transport failure accounting
remain. The user confirmed the keyboard returned and the adapter stayed stable
for60s. Fresh source/claim/USB/UART admission and procedure retention still
precede execution. This is host validation, not native, tape, key, HDMI or
normal appliance acceptance.

The unstarted procedure, post-keyboard-return read-only snapshots and helper
bytes were retained/read back before correction at
`/home/deano/fes-retained/de25-input-action-preprogram-collector-correction-20261108-r1`.
Its immutable original source/asset closure is in the preparedr1 archive.
