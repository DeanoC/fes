# Native worker bound stopped the final return

The original trial on operator source `d7e9c7337` failed and remains failed.
The 480-second native worker cap stopped it during the final Quit/menu return;
the outer wrapper did not time out. No retry or further board command followed.
This consumed the new procedure and brought the native image's total to three
attempts out of the declared maximum of three.

Verified before the stop:

- Recovery and native image programming both passed with separate, consumed
  live vendor-client scan proofs and actual programmer startup traces.
- Twelve fixed uploads/readbacks and twelve fresh final reads passed.
- The matched native boot, initial menu and shared USB ownership passed.
- ZX81, ZX80 and ZX81 launched with exact selected FPGA/ROM identities,
  positive fetch observations and logical generations 1, 3 and 5. Launches took
  54.04, 53.54 and 54.04 seconds.
- The first two stops returned to the menu in 56.67 and 56.69 seconds, with
  source/ownership/USB checks intact. Six configuration activations completed.

Final Quit was issued and the core master-stop completed at bridge generation
33. The last captured App state was `returning`, logical generation 5. The
fourth menu and final Park/generation 7 were not established by this capture.
The passive failure-tail field reporting initial RAM/decode restoration does
not prove that final native state. Current application state remains unverified;
the continuing claim is held and the user's factory-restore waiver remains.

Independent host cleanup passed: original USB permissions and the saved 15 MHz
clock readback, owned private-server exit and no UART owners. The original
failed native/experiment results and raw console remain unchanged.
[hardware-result.json](hardware-result.json) binds their hashes, verified partial
command records, both one-use grants and traces, and the cleanup evidence.

Timing identifies a host preparation problem. About 95.73 seconds elapsed
between upload completion and the native collector's start. An off-board replay
of the actual native script prefix, stopped before its attempt fuse and all
UART/JTAG activity, reproduced 100.78 seconds: `package.checked_native` consumed
100.54 seconds, including 45 HPS context worker calls. The collector itself ran
386.58 seconds including its two-second failure tail; its 450-second alarm did
not fire. No individual completed action exceeded its request bound.

The next repair should reuse the fully verified preflight result under fresh
complete source/artifact hashes, rather than repeat the same derivation inside
the timed worker. Keep the 450/480-second limits, live programming admission,
memory/firmware checks and ownership contract. This is a concrete host-side
performance repair to qualify before another hardware procedure; the consumed
trial must not replay.

The changed transport passed this attempt, but it does not establish the cause
of the earlier error 87 or guarantee future programming. Partial cycles do not
qualify the complete launch/return/park sequence, normal package admission,
physical display/input or DE10-equivalent readiness.
