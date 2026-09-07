# Appliance watchdog diagnostic: retained-RAM warm boot prevents recovery

These are diagnostic results against the designated MiSTer Pi, not acceptance
of the new appliance image. No bootstrap, rootfs, kernel or card file was changed.
The existing agent lease was held, the runtime was stopped to idle, and the
filesystem was synced before each test. A static diagnostic executable was
copied only to `/tmp`.

The executable calls `internal/bootlinux.StartGuard` and `RunGuard` from FogCast
`276eeb66b1fe3f501f0757a8290de1d37c1e1798`. Its confirmation callback always
returns false. The parent waits for the guard, then the operator script watches
for an authenticated changed boot ID and native idle readiness for 120 seconds.

| Item | Result |
| --- | --- |
| Target ID | `73dc9f5f-1a12-4a95-a820-a9b4e600769a` |
| Existing rootfs SHA-256 | `5e88021cbd58bf6177bf5432590ca36a1e3211620b09dd0eddafac571936785d` |
| Kernel SHA-256 | `a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae` |
| First test | 3-second trial, 2-second watchdog, 100-ms heartbeat |
| First boot ID | `c64604c7-fb15-4bb8-9743-4beb6fe69298` |
| First recovery | No ready boot within 120 seconds; operator confirmed manual power-cycle |
| Second test | 3-second trial, production 30-second watchdog and 5-second heartbeat |
| Second boot ID | `ca11743f-9469-4b7f-9062-3a815c529a88` |
| Second executable SHA-256 | `bff71d6482e151fe272844155d702bf148c4825569fc743db2c5728d886f34fc` |
| Second recovery | No ready boot within 120 seconds; operator confirmed manual power-cycle |

Both guards acknowledged arming and reported `context deadline exceeded` after
the deliberately shortened trial. Neither test observed automatic recovery.
Network disappearance alone does not identify the exact reset or boot stage.
The diagnostic wrapper prints the guard error but exits zero; its process exit
code is therefore not a success criterion. Only observed recovery can pass this
test.

The failed accelerated test initially suggested an inherited short watchdog
timeout might prevent boot. Repeating with the production timeout did not fix
the failure, so that explanation is insufficient.

## Controlled warm-boot diagnostic

The kernel normally requests a cold reset; the system watchdog requests a warm
reset. Pinned U-Boot enables retained-OCRAM boot by writing `0xae9efebc` to the
System Manager warm-RAM enable register at `0xffd080e0`. The live kit matched:
enable was that magic, CRC region length was zero, and execution offset was zero.
That selects retained OCRAM without a CRC check instead of a fresh SD preloader.
See the [Cyclone V boot-ROM flow](https://docs.altera.com/r/docs/683126/21.2/cyclone-v-hard-processor-system-technical-reference-manual/boot-rom-flow)
and [Intel SoCAL register definitions](https://raw.githubusercontent.com/RTEMS/rtems/5/bsps/arm/altera-cyclone-v/include/bsp/socal/alt_sysmgr.h).

Under a fresh kit lease, the next diagnostic changed only the full 32-bit enable
register to zero, checked the readback, and expired the same guard executable
with the same timings. The kit returned to authenticated native idle after
64.19 seconds. Boot ID changed from
`c87add51-857c-49fd-b1a0-89753c26a71c` to
`cace39af-3686-48cf-9741-31ce8831c860` without a requested power-cycle. The
single-variable test identifies retained-OCRAM warm boot as the recovery issue.

After boot, U-Boot had re-enabled warm-RAM boot. ROM preloader index advanced
from 0 to 1 and `initswstate` was `0x00000100`, not the successful-preloader
magic `0x49535756`. Production preparation must both record the completed
preloader and disable retained-RAM boot before arming the watchdog, so repeated
fallback does not exhaust the four preloader copies.

FogCast `980be19710a5e1ab3d5f98ab74d01eb51509844d` adds that preparation with
board gating, register readbacks, focused tests and independent MMIO review.
A diagnostic executable calling the production helper and guard returned to a
new idle boot in 64.23 seconds; the preloader index stayed at 0. Its SHA-256 is
`d847b042f182d7c89b403513d940b23e90457324ccf28c70503ba24302dfe52a`.
Repeated-reset and sustained-stability validation remain pending. A subsequent
lease claim was refused because `yosys-mlab-init` owned the kit for development-
RBF acceptance; no takeover or further reset was attempted. The operator
confirmed no additional manual power-cycle during an intervening boot, so that
boot cannot be attributed to manual recovery and may belong to the other task.
These diagnostics do not accept the full bootstrap/update path.

Raw local diagnostic records are retained under
`out/dev/appliance-release/watchdog-diagnostic.json` and
`out/dev/appliance-release/watchdog-production-timeout.json` in the original
integration checkout; the successful controlled test is `watchdog-sd-boot.json`
in the same directory. They contain health identity and lease metadata, not an
authentication token. New source-bound card artifacts separately retain
`hardware: not-run` until exact-artifact acceptance is performed.
