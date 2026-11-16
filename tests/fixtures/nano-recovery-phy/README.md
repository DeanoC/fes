# Pinned recovery PHY fixture

These unmodified files come from the Terasic U-Boot commit
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`. Their SPDX/copyright headers remain
intact. `platform/embedded/nano/recovery.py` locks their complete SHA-256 values.

Tests reconstruct the pinned files after transformation and execute the original
and changed first-start paths against the same PHY model. They do not establish
physical link or network acceptance. This is a historical test fixture, not a
second production driver.
