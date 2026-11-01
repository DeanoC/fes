The audit in `receipt.json` selects producer commit
`786e6383b32617af8baeb32e9de6cf99231a19f9` and the generated project at
`out/de25-zx81-hps-786e6383b3/hps/project`. It records source and generated-HDL
evidence. Compile/timing results and hardware acceptance are separate checks.

The audit compared all 40 captured files with their selected Git blobs, all
25 staged files with the input manifest, 497 original HPS/adapter/EMIF
parameters, 53 HPS/EMIF top connections, and four vendor software files.
The original boot SPL remains byte-identical. Retaining these inputs does
not establish boot-firmware or board acceptance.

The generated endpoint uses the retained 100MHz peripheral clock and its
two-stage reset controller. Its independent pixel input runs at exactly
74.25MHz. Generated PLL counters reproduce both rates exactly from their
50MHz references. The synthesis report records the nonzero QSF BUILD_ID
macro reference and the matching elaborated parameter prefix; the generated
wrapper leaves the leaf parameter default intact. The fingerprint identifies
the captured source/vendor/recipe inputs and is not an authenticated package ID.

The selected MIF defines 12288 bytes; the simulation HEX contains 16384 bytes
and matches every MIF-defined byte. The default ZX81 ROM address ties bit13
low, so only the low 8192 bytes are reachable. Those bytes match across the
root, staged and generated MIF and the simulation HEX, with SHA256
`14ad84f4243efcd41587ff46ab932d11087043e8d455a1ed2a227b9657828dfa`.
The inherited 16384/12288 depth warning concerns the unreachable tail for
this model. This is a source address proof, not a hardware readback.

Replay from the task worktree root, retaining the producer output directory:

```sh
python3 -B sources/misteross/docs/validation/2026-10-29-de25-zx81-hps/generated/audit.py \
  --build-dir out/de25-zx81-hps-786e6383b3/hps \
  --output /tmp/de25-zx81-hps-generated-audit.json
diff -u sources/misteross/docs/validation/2026-10-29-de25-zx81-hps/generated/receipt.json \
  /tmp/de25-zx81-hps-generated-audit.json
```

The script reads Git and retained producer files and invokes no Quartus or
hardware operations. Moving the output directory changes the recorded
absolute directory. If a tool regenerates a hashed report, preserve the old
receipt and review the new report before treating the hash difference as expected.
