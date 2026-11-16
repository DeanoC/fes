# Mixed instruction residual experiment (private; diagnostic only)

The frozen source at shared commit ab752c6e produced three whole-loop residuals in `../original-mixed/comparison.json`: BYTE_STREAM -1.022, ARITH_BRANCH +1.007 and ENTER_LEAVE +1.994 clocks relative to the 86Box 12.5 MHz reference. These original misses are preserved. A separately derived experimental candidate changes unprefixed STOS, immediate TEST and 16-bit-stack zero-nesting ENTER/LEAVE costs. It remains disabled by default and is not a product fix.

This follow-up compares the private old CPU a583e34c4ec711e86ed384f1a41386df4125cd919c63d9dc81869b6a2e46565b to the separately built v2 CPU d72449f75939c204877dcaf10af10492e52799dee958631d08bba967def27db6. Predicted RTL inputs were read-only, hashed before and after, and completed before reference results were read. All sixteen paired instruction bodies, including branch displacement, match byte for byte. The v2 stack-width metadata was added after static review; its candidate predictions match the earlier private candidate for these 16-bit-stack loops.

One private reference boot ran three copies of each 64-tick guest measurement. All three result files are byte-identical and include RAM_COPY_VERIFIED. All 1,320 continuous 4 fps samples contain the identical visually verified 100% indicator; this does not prove emulator speed between samples. Frozen 3,102-entry input manifests match before and after. Raw video SHA-256 90d6df2d788745a62273ede342f5fdace1ebf62add7696d96ff984a65738ab04 remains on the private 3090 host under /tmp/fes1902-residuals-prospective; the compressed indicator strips are retained locally.

Fifteen out of sixteen whole-loop comparisons differ from the v2 prediction by at most 0.036 clocks. The pre-specified nested ENTER level-1 negative control fails: 86Box 32.050 vs v2 28.029 clocks per whole loop (old 29.029). This is not a per-instruction isolated result; both measured windows include timer ISR and differing block setup. Quantization is not the full error bound. The nested miss is retained, not treated as covered.

The subsequent protected-mode stack test in `../nesting/controls/stack/` confirms preserved prior diagnostic charges for SS.B=1 and active rules for SS.B=0, immediately after reloading SS in both directions. It checks general-purpose state and stack RAM, not interrupt/fault, MMIO/crossing, default-off netlist identity, hardware, actual game pitch or tier-1 acceptance. The focused module bench passes on v2 including 32-bit-stack, prefix, non-RAM, REP and nesting fallbacks.

Reference CPU model is a surrogate for the named emulator, not physical 80286 timing. Captures do not qualify a sealed artifact or kit. The subsequent nesting study is in `../nesting/`; deferred-byte fault behavior remains under investigation.

The original `reference-vm/frozen-before.json` and `reference-vm/frozen-after.json` were byte-identical and are now represented by the single retained `reference-frozen-manifest.json.gz`. Run `python3 verify-manifests.py` from this directory to reconstruct both in temporary storage and verify their original sizes and SHA-256 values. The original raw files remain in the published Git history.

## Selected SHA-256 identities

- `paired-loop-byte-manifest.json`: `d0bbef006549879808c9dc103f352b60f9b199511fc9bf4b42191e848b391693`
- `comparison.json`: `c7aea88ebb8218cea2f7ba46efb25ef430d3b36142ef0d2441900f67ed799454`
- `frozen-old/rtl.json`: `e4db95f01f9b1b240bd767840307da6e60b076a73250eab2c5ca8141cac22c8e`
- `frozen-v2/rtl.json`: `338bb09e296e7232acf6704504d6fd452dc0217713dbfc5a916f857fdaade047`
- `frozen-v2/input-before.sha256`: `698e297bca3c330234cfe70f71b682af3e0d409b2640f6c33a78eac86c0c59d9`
- `frozen-v2/input-after.sha256`: `698e297bca3c330234cfe70f71b682af3e0d409b2640f6c33a78eac86c0c59d9`
- `reference-vm/RES1.TXT`: `3751f26d6a5ee1f6b5ae89b6a5531e51ab73b0b8235dc9d21534f847d1515396`
- `reference-vm/speed-audit.json`: `0b01d4c6e8ab206d4a6f0cfde074a7ec08e102dc3ce1b363fb7c6841aa6a1c4d`
- `reference-vm/frozen-before.json`: `92b7bd0e462eb25993bcde5e74d6231d6ac7569f1cd2bf532ad70f3155552ff8`
- `reference-vm/frozen-after.json`: `92b7bd0e462eb25993bcde5e74d6231d6ac7569f1cd2bf532ad70f3155552ff8`
