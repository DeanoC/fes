# Crash-safe A/B OTA manager (`ota/`)

`ota` provides a power-fail-safe A/B update state machine over any FAT32 volume
implementing `FileSystem` (such as `*fat.Volume` or `*storagepolicy.Volume`).

## On-disk layout (`FES/OTA`)

- `FES/OTA/STATE.JSN`: durable state (`format`, `generation`, `active_slot`,
  `pending_slot`, `phase`, `tries_remaining`, `slot_a`, `slot_b`).
- `FES/OTA/SLOTA/IMAGE.BIN` and `FES/OTA/SLOTA/META.JSN`: slot A image & manifest.
- `FES/OTA/SLOTB/IMAGE.BIN` and `FES/OTA/SLOTB/META.JSN`: slot B image & manifest.

## State machine and crash safety

```text
  idle ──Stage──▶ staging ──write IMAGE.BIN + readback + META.JSN──▶ staged
   ▲                 │                                                  │
   │          (power loss -> SelectBoot                          Activate
   │           clears to idle on active)                                │
   │                                                                    ▼
   └─────────── ConfirmBoot(pending) ◀──── trial ◀── SelectBoot ─── pending
   ▲                                         │   ▲                     │
   │                                         └───┴── auto/manual ──────┘
   └────────── Rollback / tries == 0 / bad SHA-256 ────────────────────┘
```

All metadata and payload updates use copy-on-write `ReplaceFile` + flush. Before
modifying the inactive slot, `Stage` persists `phase: "staging"`. Any crash
before `phase: "staged"` is recovered by `SelectBoot` back to `idle` on the
unmodified `ActiveSlot`. Before every trial boot, `SelectBoot` verifies
`META.JSN` and the full `IMAGE.BIN` SHA-256 on disk; mismatches or exhausted
`TriesRemaining` automatically roll back to `ActiveSlot`.
