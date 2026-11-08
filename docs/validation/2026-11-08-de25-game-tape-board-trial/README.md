# Bounded initial-tape native trial preparation

The new matched native SDK, entry, BL31, SPL/FIT and composed SOF are built from
`ba2f1ef5e08cbfb42d94f5fcf42c3e807b3534bc`, including the retained game-tape
implementation. Boot identity is `71ae1d6bc14ee11d`. The FIT is 8,288,800 bytes,
with 99,808 bytes remaining inside the original 8 MiB bound. The entry grew by
4 KiB; Go RAM, configuration/menu arenas, device maps and firmware OCRAM limits
are unchanged. This new image has no inherited hardware acceptance.

The tape is the existing simulator's original BASIC line `10 REM A`. An isolated
copy of the RTL and same ZX81 ROM saved its complete 917-byte image, cold booted,
loaded it and verified the line returned. An export-only C++ addition writes
that validated image. SHA-256 is
`3664f171999bf597cc284754ac8f7ad2071bafd78fb1cd48c330d82dfbebaabb`.
Its odd size crosses a media chunk boundary. The original wrong-working-directory
simulation failure is retained separately; the unchanged compiled model passed
with its private ROM in the correct directory. This is simulated ROM consumption.

The real new SDK and two distinct sealed ZX81/ZX80 archives/ROMs pass retained
preflight with this tape and zero firmware calls, using a virtual Go test file
without editing any tracked build inputs. Actual paired collector exchanges and
saved receipt joins pass with selected tape receipts; missing, changed and
malformed receipts retire without replay. The expanded uploader passes 136
stateful runner cases, including all thirteen fixed files, fresh readbacks and
failures. Its original missing synthetic tape-input failure is preserved; the
corrected fixture includes the exact 917-byte tape. Programming, transport,
receipt and master cleanup fixtures also pass.

The unchanged full native validator passed before file reuse was prepared.
The frozen file-only record includes the selected assets and tape as explicit
inputs. The actual timed worker reloads the complete source/artifact closure in
0.77 seconds with no HPS derivation worker, UART access or attempt reservation.
Six stale/forged prefix cases reject before those boundaries. This record grants
no live access; source, claim, USB/UART and one-use programming admission remain
separate checks.

The [frozen operator](operator-source.json) permits one attempt on these new
compiled bytes, with zero prior attempts on this image. The preceding image's
four consumed attempts and complete paired-cycle result remain independently
retained. There is no replay or retry after a failed hardware proof.

The procedure uses top TRWU8CBL only and its existing spare card/claim. It runs
one qualified recovery, thirteen fixed uploads/readbacks and thirteen fresh
reads. The first ZX81 selection includes `FES/GAME.P`; ZX80 includes no tape.
Native Tenfoot must complete ZX81 → menu → ZX80 → menu → ZX81 → menu and final
Park: three launches/returns, four menus, seven configurations, logical
generation 7 / bridge generation 40. Every active tape receipt must equal the
selected hash/size, and return must clear it. Physical ROM consumption is not
established by that transfer receipt or the off-board SAVE/LOAD simulation.

All existing bounds remain: startup/launch/menu restore 100 seconds;
configuration 20; core close/final Park 5; programmer 90; live scan 30 inside
that envelope; native collector 450/process 480; uploads 300/process 320;
recovery 270; outer 840/host 890. Console capture is at most 1 MiB, programming
traces 8 MiB, passive failure tail two seconds. First failure stops later phases.
Clock restoration, exact USB permissions, private-server shutdown and free UART
must be checked independently after the attempt. Factory restore remains waived;
the bottom kit is excluded.

Root read-only preflight, committed operator checks and complete prepared-chain
archive/readback must pass before programming. These preparation results prove
neither physical tape loading nor normal catalog/appliance readiness. Visible
output, combined physical controls, native networking, FPGA timing and broader
admission remain open.
