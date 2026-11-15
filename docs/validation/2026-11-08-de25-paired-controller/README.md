# DE25 paired native controller and matched boot chain

Base `31251509f93b8d51bee8b677defda79df9eb7a76`; implementation `38a2d9108ca0c9634f85e2ecd15a889ee24a8f83`. This milestone joins the selected
native menu and computer lifecycle in a bounded, injectable board-test controller.
It changes host tooling and tests. Native runtime, firmware source and shared
hardware contracts are unchanged.

The controller checks the actual paired startup marker and CPU checkpoints,
source-derived companion/union mapping, all menu configuration/open/frame/drain
records, selected core configuration/fetch, shared USB boundaries and final
Parked generation 7. It derives subsequent configuration generations from the
verified preceding return. Repeated transport polling consumes no menu evidence.
Every failed or uncertain exchange retires the client without a second command.
The original core-only controller remains strict. Core close is still bounded
to five seconds; independent menu restore and final park have separate deadlines.

Validation: 1667 affected parent tests passed (3 skips); 14 focused
methods cover complete UART, gamepad and keyboard flows, active/already-drained
closures, corrupt/missing/reordered evidence, lost ownership and phase deadlines.
The controller wire is synthetic, assembled from retained C observations;
separate existing tests exercise real Go menu telemetry. These are host checks.

All 2105 selected native source hashes and
44 entry-producer source hashes still match the retained
executable/entry, including freshly checked sealed sources. The matching BL31,
SPL and FIT have now been built and joined by byte identity and resource maps.
FIT size: 8284720 bytes of 8388608, leaving
103888 bytes. Loaded low firmware ends at
`0x5de00` within `0x60000`.
The original computer descriptor reserves 2 MiB; only the checked menu companion
supplies the explicit 8 MiB union. The exact executable includes the menu observer.
These are compile/source/memory checks, not native boot or physical acceptance.

The raw logs, commands, source bundle and chain artifacts are recorded in
`out/nano-paired-controller-20261107` and the retained location in `result.json`.
No kit, card, JTAG, Quartus/PFG, network upload, PR, push or merge occurred.
The next step is to compose the matched image, bind the new controller into the
single-attempt operator, re-admit TRWU8CBL and perform the bounded paired trial.
Broader cores, media, audio, network and full appliance acceptance remain open.
