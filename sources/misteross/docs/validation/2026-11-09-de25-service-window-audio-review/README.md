# Scanout service-window audio-reference review

The first standard ZX81 fit at `88cbcf79c480745d37aeecf744f0156367623cf8`
completed compilation, then the canonical package producer refused its stale
audio-source pins. That failed candidate remains rejected: it has no independent
repeat or exported package.

The reviewed changes from `5870674ab5766e2e638b5d78916d01d991026327` affect
three pinned inputs. The standard wrapper and component Tcl widen the scanout
master's burst count from five to seven bits. The builder changes only its
verification of that width and of the memory crossing's 64-beat/512-response
configuration. The other 31 builder functions have identical Python ASTs,
including audio-clock generation, reset/hold wiring, PCM and expansion tie-offs.
The five other pinned audio RTL/SDC inputs are byte-identical. `review.json`
records both sets of exact source hashes.

The unchanged fitted observation tool inspected a private copy of the actual
first-fit database. Its four corners and 32 fitted paths give positive
conditional receiver budgets: minimum setup 159.193 ns and minimum hold
159.497 ns. The receipt binds the actual source, RBF, database, reports, tools,
stock pins/voltage/PLL and vendor receiver reference. These engineering
observations do not admit the failed package or qualify PCB timing or hardware.

This refresh changes only the three exact source pins. The user-approved
exception remains confined to `HDMI_SCLK` and `HDMI_LRCLK`. Eligibility checks,
raw warnings, every numerical check, fitted review and source/tool/execution
seal remain required. A new canonical producer must independently build and
qualify both fits before export; hardware acceptance remains separate.
