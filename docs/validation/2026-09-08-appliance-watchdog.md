# Appliance watchdog diagnostic: recovery not established

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
| Second recovery | No ready boot within 120 seconds; power-cycle requested |

Both guards acknowledged arming and reported `context deadline exceeded` after
the deliberately shortened trial. Neither test observed automatic recovery.
Network disappearance alone does not identify the exact reset or boot stage.
The diagnostic wrapper prints the guard error but exits zero; its process exit
code is therefore not a success criterion. Only observed recovery can pass this
test.

The failed accelerated test initially suggested an inherited short watchdog
timeout might prevent boot. Repeating with the production timeout did not fix
the failure, so that explanation is insufficient. Reset-manager and boot-ROM
warm-reset handling remain under investigation. Do not repeat destructive reset
experiments without a specific hypothesis and an available recovery path.

Raw local diagnostic records are retained under
`out/dev/appliance-release/watchdog-diagnostic.json` and
`out/dev/appliance-release/watchdog-production-timeout.json` in the original
integration checkout. They contain health identity and lease metadata, not an
authentication token. New source-bound card artifacts separately retain
`hardware: not-run` until exact-artifact acceptance is performed.
