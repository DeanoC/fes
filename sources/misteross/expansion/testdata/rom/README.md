# Synthetic M10K ROM oracle

These fixtures contain no machine ROM or routed design. Mistral compiled a
minimal `5CSEBA6U23I7` device with eight explicitly blank 1024x10 INIT blocks
at column 5, rows 73–80, and five synthetic ROM patterns. They qualify INIT
placement, bit permutation, framing and checksums, not a working ZX81 core.

`blank.rbf.gz` is the base; `map.json.gz` binds its SHA256 and enumerates stored
INIT bit destinations. `*.rom.gz` contain the exact test bytes, including the
seeded random case. `oracle.json` records golden Mistral RBF hashes, sizes,
source database/tool hashes, exact readback and unchanged non-INIT CRAM checks.
Gzip members use mtime zero. No kit requires these Python scripts or Mistral.

Regenerate from the misteross module with a matching selected Mistral binary
and source checkout (the recorded qualification used cache slot
`ddcd49051df430f25b58a05011bc3bf0379a33cf482384da288703d18bd1a8f4`):

```sh
python3 scripts/rom_map_oracle.py \
  --mistral-cv /selected/slot/install/bin/mistral-cv \
  --mistral-source /selected/slot/src/mistral \
  --output /tmp/fes-rom-oracle \
  --fixtures expansion/testdata/rom
```

The optional Python oracle test accepts the same paths via
`FES_ROM_ORACLE_MISTRAL` and `FES_ROM_ORACLE_SOURCE`. Ordinary Go tests use the
checked-in gzip fixtures without a compiler/tool dependency.
