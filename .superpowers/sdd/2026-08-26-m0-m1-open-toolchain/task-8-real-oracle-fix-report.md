# Task 8 corrective report: genuine Quartus 17.0.2 report compatibility

## RED

Command:

```text
python3 -m unittest -v tests.test_oracle_boundary
```

Result: 16 tests ran with 4 expected failures. The failures reproduced the
observed gaps: multiple operating-corner Restricted Fmax rows were rejected,
capability/entity/pin prose caused false hard-resource failures, the banner was
stored instead of the exact version line, and the oracle QSF still used the
ignored `INCREMENTAL_COMPILATION OFF` assignment.

## GREEN

Focused command:

```text
bash -n scripts/build_oracle.sh && python3 -m unittest -v tests.test_oracle_boundary
```

Result: 16 tests passed.

Full-suite command:

```text
python3 -m unittest discover -s tests -p 'test*.py' -v
```

Result: 177 tests passed.

Additional local parser exercise using the generated Quartus report text and a
fake Quartus executable produced `321.34 MHz`, measured zero RAM/DSP/PLL,
null static MLAB/LUTRAM and HPS counts, no unknown resources, and the exact
`Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition` provenance line.

`git diff --check` passed.

## Files changed

- `scripts/build_oracle.sh`: require one exact 17.0.2 version line; parse
  physical hard-resource rows by exact labels and fitted-summary structure;
  accept the conservative minimum across valid operating-corner Fmax tables;
  ignore capability/prose rows while retaining fail-closed malformed and
  contradictory evidence handling.
- `experiments/010_blinky/oracle/top.qsf`: replace ignored incremental
  compilation assignment with `IGNORE_PARTITIONS ON`.
- `tests/test_oracle_boundary.py`: add synthetic regressions for all Task 8
  report, provenance, and QSF requirements.

## Commit

`9482973d6fd72781ad805c95a5d018c37f466890`

## Concerns

No repository concerns. Per the task boundary, no fresh hardware/network
target access or Quartus recompilation was performed; the parser was exercised
against the captured generated report shapes through hermetic fake-compiler
tests.
