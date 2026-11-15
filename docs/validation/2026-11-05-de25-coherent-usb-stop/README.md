# Repaired shared USB/core-load attempt: guard failure before confirmation

TRWU8CBL, compiled inputs `1c84ce4aa9941f94353b9879dbd8d02402986c70`, operator checkout `609602b9c`. One attempt, no retry. The application/entry reuse is byte-verified in the matched candidate evidence.

Recovery, seven fixed uploads and fresh SD readbacks passed. The matched native image reached Tenfoot Ready with all 14 CPU checkpoints, complete source/pin observations and the expected gamepad descriptor. Seven physical reports arrived before the first owned generation-2 USB pause.

The first ZX81 load progressed through both payload writes and two completion polls. The final mailbox observation reported all 1,650,688 payload bytes retired with no bytes pending. The native guard then reported `configuration deadline or backwards clock` before STATUS/COMPLETE, endpoint acceptance, USB resume or any return/quit. The service entered `reboot_required`, generation 1, with resources retained. Byte retirement is not successful configuration or core activation.

The first write observation took 4.988 seconds since arm, including 2.482 seconds hashing and 2.506 seconds outside hashing. The previous shared image took 10.101 seconds, including 2.482 seconds hashing and 7.620 seconds outside hashing. The repaired image's final poll was 16.211 seconds since arm, including 7.194 seconds measured hashing; the complete configuration interval and guard's software-clock reason were not logged. Physical counter observations are increasing. These measurements show progress and lower overhead, not a passed deadline or proof that the combined error message specifically means elapsed-time expiry.

The original trace is immutable. Its failure-marker check also interrupted passive-tail collection, leaving a partial panic stack; CPU halt is not proved. Independent audit verifies the cold/App/USB startup and first failed launch. The operator sent no further key, reset, retry or recovery command.

Host cleanup passed: unchanged15M, exact USB permissions, private JTAG stopped, UART free. Test card and exclusive claim remain; original factory card is untouched. The board is parked with uncertain configuration resources retained. `retention.json` binds the trace, procedure, read-count model and separately retained matched candidate.

Next work is to distinguish elapsed-time expiry from a backwards software clock in the native error observation, correct receive-only failure-tail logging, and investigate repeated board clock/pin preparation while preserving fresh checks across actual operations. The 20-second configuration limit stays intact. No ordinary package/ROM/media, physical input/video or DE10-normal-kit readiness is claimed.
