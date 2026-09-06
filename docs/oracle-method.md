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

Rebuild a fetched core with the same Quartus install:

```sh
export QUARTUS_ROOTDIR=/path/to/17.0/quartus
make fetch-core CORE=megadrive
make rebuild-core CORE=megadrive
```

That compile runs in `build/rebuild/megadrive/project/`, never in the fetch
checkout. The staged `sys/build_id.tcl` honors `MISTER_BUILD_DATE`, defaulting
to `260603` from `releases/MegaDrive_20260603.rbf` (`--build-date` overrides).
The rebuild is a second artifact beside the locked upstream RBF. They are not
required to bit-match; Lite Edition cannot reproduce a Standard Edition
bitstream. The Lite Mega Drive rebuild has been loaded on real MiSTer
hardware. `make select-core` selects that rebuild;
`ARTIFACT=upstream` falls back to the official release.
