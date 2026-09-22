# Quartus reference lane

Quartus Prime Lite 17.0.2 is an optional reference compiler. It is never an
OSS or simulation dependency and this repository does not download or install
it.

Point `QUARTUS_ROOTDIR` at either the Quartus directory containing
`bin/quartus_sh` or its versioned parent:

```sh
export QUARTUS_ROOTDIR=/path/to/17.0/quartus
make oracle EXP=010_blinky
make oracle EXP=020_linux_mailbox
make oracle EXP=050_lut_mul
make oracle EXP=060_dsp_mul
make oracle EXP=070_mixed_mem
make oracle EXP=080_dsp_mem
make oracle EXP=100_dsp_rom
```

The wrapper accepts only version 17.0.2, stages the selected minimal project
under `build/oracle/<experiment>/project/`, and runs:

```text
quartus_sh --flow compile top
```

The resulting RBF is copied to:

```text
build/oracle/<experiment>/top.rbf
```

Compare it with an existing OSS build using:

```sh
make compare EXP=010_blinky
make compare EXP=020_linux_mailbox
```

The compilers are not expected to produce byte-identical RBFs. Comparison
checks the selected experiment, target, source identity, resources, timing,
and successful artifact generation.

Normal FES package production uses the authenticated functional-identity-2
HIP/nextpnr recipes. There is no fetched Mega Drive rebuild, raw-core selector
or upstream-RBF fallback. Historical oracle comparisons remain evidence for
the artifacts they named, not a supported product build path.

Explicit FES Quartus recipes remain bring-up/oracle checks where documented.
They do not replace a failed normal package producer or become launch inputs
automatically. The splash/idle firmware seal retains its separate diagnostic
record schema and build policy.
