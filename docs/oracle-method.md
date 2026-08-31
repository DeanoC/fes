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
