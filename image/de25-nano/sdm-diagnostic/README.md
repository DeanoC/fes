# DE25 SDM-error boot diagnostic

This off-board package replaces BL31 in the exact Terasic SD FIT. It is an
unqualified diagnostic, separate from normal image construction. The stock
vendor lock and all normal image checks remain unchanged. This tool neither
opens a device nor writes an SD card, QSPI or FPGA.

The [locked source](firmware.lock.json) binds Terasic TF-A revision
`580dcb5c4e10aa7daff2bf69d493c076a6d3e665`, the complete before/after service
source, [correction](completion-errors.patch), compiler inputs and compiled
binary/ELF. Two clean builds produce identical binary and ELF. The actual
compiled SiP handler passes eight instruction-level cases with injected mailbox
replies. Fourteen additional instruction-level cases verify the remapper guard,
including failing controls for the previous binary. These checks do not exercise
EL3 mappings, SDM, DMA or a native boot.

The correction reports hard mailbox errors even with an empty response or a
previous buffer completion in the same call. It logs the original error,
reports no reusable buffers and sends no further data after the error. It does
not abort SDM, fence DMA or clear the kernel's retained-failure state. No board
trace has yet established that this ambiguity caused either static-load failure.

Configuration start also calls the vendor remapper initializer without an
address. The previous compiled helper read and wrote through that null pointer.
The guard preserves the initializer's remapper-register writes while translating
an address only when one is supplied. The instruction proof verifies both null
initialization and unchanged valid-address translation for all four scratch-bit
and bypass combinations. The earlier native START returned successfully; an
emulated unmapped-page fault does not establish a physical fault or explain the
upload stall. See the [source-bound proof](../../../docs/validation/2026-11-05-de25-static-expansion-offboard/sdm-remapper-guard.json).

The previous completion-only diagnostic booted on the board, but its single
static-core launch still failed during full-base upload, before persona or BASIC
testing. The original working card and host were subsequently authenticated and
the board released. That failed candidate remains retained. The current
remapper-guard candidate has no native boot or programming acceptance.

Prepare from the exact stock FIT and the retained, independently rebuilt
candidate artifacts:

```sh
python3 scripts/de25_sdm_diagnostic.py prepare \
  --vendor-fit /absolute/vendor-boot/u-boot.itb \
  --bl31 /absolute/candidate/build/agilex5/release/bl31.bin \
  --elf /absolute/candidate/build/agilex5/release/bl31/bl31.elf \
  --output /absolute/new/sdm-diagnostic
python3 scripts/de25_sdm_diagnostic.py verify --bundle /absolute/new/sdm-diagnostic
```

The new firmware fits inside the original external ATF slot. Only its bytes,
data-size and CRC32 change; unused slot bytes are zeroed. U-Boot and its DTB,
their offsets/hashes, every configuration property (including the vendor's
unpopulated signature placeholder), load/entry addresses and total FIT length
are preserved. Preparation authenticates the entire stock template and both
candidate artifacts. Verification independently reconstructs the candidate and
refuses changed, extra or symlinked files and any admission claim.

`stock-u-boot.itb` is the exact rollback FIT, not a board readback. The bundle
does not contain an SD writer or grant permission to replace a boot file.
Before a physical trial, verify the selected card, preserve the designated
working baseline, obtain explicit approval for this corrected-firmware trial,
and authenticate the new `fes-de25-sdm-remapper-580dcb5c4e10` boot marker before
submitting a core load. Keep all failed-transfer retention and restoration gates.

The [dated validation](../../../docs/validation/2026-11-05-de25-static-expansion-offboard/README.md)
distinguishes the stock-binary ambiguity proof, correction tests, packaging and
remaining native acceptance. The diagnostic does not satisfy the normal-kit
goal until boot, static core use, USB/audio and recovery pass physically.
