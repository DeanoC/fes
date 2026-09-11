# Appliance first-boot expand — implement cut

Status: `GREEN` for landing the host expander and the FogCast agent bind
helper. This is not board-boot acceptance of the expanded spare and does not
touch the living-room `0.2.0-dev.4` card.

Validation ran on 2026-09-11 from `ai-dev-mac`. HOLD Redeem was lifted for this
burn. No spare physical rewrite was required: the 2026-09-10 extra-p3 HIL
already formatted `FESDATA3` on the exact USB spare, and this cut adds no new
MBR or mkfs behaviour.

## FES

Landed on `feat/first-boot-expand-impl`:

- `scripts/appliance_expand_dry_run.py` — sparse-file plan/apply, exact USB
  `WRITE_GO` write-base/apply/verify, physical grow-FAT rejected
- `tests/test_appliance_expand_dry_run.py` — 12 tests
- design, spike, and spare-card HIL notes from 2026-09-10

Assembly still produces fixed `de10-nano-appliance-1g-v1` (`card.img`
1,075,838,976 bytes). Expanded cards remain a live mutation.

Focused checks:

```text
python3 -m unittest tests.test_appliance_expand_dry_run -v
Ran 12 tests ... OK
```

Live card identities treated as read-only context:

| Item | Value |
| --- | --- |
| Live card artifact | `67297042698dc5be832e700a0eef887bf63454d489c0f4e470f6f9300bd92065` |
| Release rootfs | `1a01b9100b7a0fa70f8f69a7c2d18ecffed22be9396e2496b1ae805304781c32` |

No `kit.py` claim, no mister boot, no USB `WRITE_GO` this cut.

## FogCast

The sibling FogCast branch `feat/first-boot-expand-impl` adds
`internal/appliancedata` and calls it from `mister-agent` startup before the
cache, saves, core-data, launcher-cache, or evidence trees are opened.

- If `/dev/disk/by-label/FESDATA3` or labelled `/dev/mmcblk0p3` exists, mount
  it at `/run/fogcast/fesdata3` and bind those five directories.
- Copy FAT files that are missing on p3 first. Never overwrite p3 files.
- Never bind or copy `releases`, `agent.toml`, `launcher.json`, or `target-id`.
- Refuse the bind while `GET /v1/update` would show trial, pending, or corrupt.
  The agent still starts and keeps those trees on FAT.

Host/frontend code is unchanged. No `@codex` review is requested on this path.

## Not this cut

- Mutating live `.4` in the DE10 slot
- Rewriting the spare USB card
- Board boot of the expanded spare (exclusive `kit.py` lease only if that HIL
  is later authorized; live `.4` stays in the kit)
- Automatic GC of unreferenced release images
- Grow-FAT
