`generated-receipt.json` binds the repaired full producer
`5a7382c4b5c17173f48bd074847acc0acc22de93` at
`out/de25-zx81-hps-5a7382c4b5/hps`. It supplements the earlier failed producer's
audit, which remains unchanged.

The existing audit passes for all 41 selected Git blobs, 26 staged files,
497 original HPS/adapter/EMIF parameters, 53 HPS/EMIF top connections and four
vendor software files. Generated clock/reset routes and exact PLL counters
remain 100 MHz system/74.25 MHz pixel; synthesis uses BUILD_ID
`a6b159e617d400d212240b31944cf377`. The reachable low 8192 ROM bytes retain SHA256
`14ad84f4243efcd41587ff46ab932d11087043e8d455a1ed2a227b9657828dfa`.

Captured, staged and generated tape adapters all match
`f8f9c1b7635d33853cca4f69a41b0a1f7d5afcec32ea00c1c7dfa7a5ad08361f`,
the exact template tested by the retained vendor-model differential. The
native branch contains the complete portable source verbatim. Full-project
SYN rows 19250-19251 now list two 8192x8 SimpleDualPort M20K banks, 65536 bits
each; their shared parameter table retains mixed-port `OLD_DATA` and CLOCK0
read-address/control registers. Full synthesis estimates 13378 ALMs and 23469
logic registers. These are synthesis and source checks; routed timing and
hardware acceptance remain separate.

Replay the existing audit from the task worktree root:

```sh
python3 -B sources/misteross/docs/validation/2026-10-29-de25-zx81-hps/generated/audit.py \
  --build-dir out/de25-zx81-hps-5a7382c4b5/hps \
  --output /tmp/de25-zx81-hps-5a7382-generated.json
```

This produces the base fields of the new receipt. The additional
`repaired_tape` object records the exact native-branch/source checks and
full-project mapping rows. Its report and adapter hashes are included in
`evidence_sha256`; the input manifest hashes bind the captured source. Root's
compile/STA evidence covers the rest of the full-board build.
