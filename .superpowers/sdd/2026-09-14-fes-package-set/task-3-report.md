# Task 3 evidence: explicit FES package set

## Scope

Task 3 updates the default profile and the parent contract documentation after
the independent Task 2 repair review. The default
`native-integration-dev` profile now selects, in order:

~~~text
fes.pong
fes.zx81
fes.coleco
~~~

The documentation describes the closed package-only image contract, the
authenticated HIP/nextpnr producer and shared cache as the normal FES route,
and Quartus/format-1 as separate historical or oracle-only concerns.

## TDD evidence

Red was captured after adding the focused profile and documentation assertions:

~~~text
python3 -m unittest tests.test_core_build tests.test_image_assembly
Ran 55 tests
FAILED (failures=2)
~~~

The failures were the still-Pong-only profile and stale Pong-only documentation
language.

After updating the profile, documentation and assertions:

~~~text
python3 -m unittest tests.test_core_build tests.test_image_assembly
Ran 55 tests in 0.051s
OK

git diff --check
PASS
~~~

The focused tests cover the ordered profile IDs, package-only mode, all three
package names and the documented HIP/nextpnr, closed-set, Quartus and
format-1 boundaries. A stale-contract scan found no remaining Pong-only or
single-package wording in the updated default-path documentation.

## Changed files

- `profiles/native-integration-dev.toml`
- `README.md`
- `docs/core-packages.md`
- `docs/getting-started.md`
- `docs/development.md`
- `tests/test_core_build.py`
- `tests/test_image_assembly.py`

Task 2's independent review was SPEC PASS / QUALITY PASS with no P1, P2 or P3
findings. This task does not claim a cold image build, Quartus execution, QEMU
acceptance, or physical hardware acceptance.
