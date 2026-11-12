# OpenROM builder audio-reference review

The OpenROM adapter changes the pinned ZX81 builder from the qualified
`5bf7202ca338754f2f7966b522306ac7992d4206` control to
`bc36e7c45078249988253115029ffe2c2cac7881`. The first current-source fit
completed, then package qualification correctly refused the old builder hash.
That failed candidate remains rejected; this record does not export it.

Codex reviewed the builder diff and the source-bound first-fit observations.
All seven other frozen audio RTL, wrapper and SDC inputs still match their
approved hashes. The six audio integration functions have identical Python
ASTs; the actual compiled `golden_top.v` is byte-identical to the qualified
control. The only changed functions capture or generate OpenROM, replace the
historical ROM conversion, or preserve the native tape branch containing its
new conditional. The selected Quartus tape implementation is unchanged.

The unmodified fitted observation tool ran against a private copy of the
candidate's database. Its report inventory/hashes, parsed observations and
positive conditional receiver budgets were checked again locally. The
receipt retains the actual tool, source, fit/RBF, stock pin/voltage/PLL and
vendor receiver-reference bindings. `review.json` records the builder hashes,
unchanged functions and compiled-top identities.

This refresh changes only the frozen builder hash. The prior user-approved
exceptions remain exactly `HDMI_SCLK` and `HDMI_LRCLK`; every numerical check,
raw warning, fitted review, source/tool/execution seal and independent rebuild
remains required. The next canonical producer must rebuild and independently
qualify both fits; these first-fit observations are not that qualification.
PCB trace skew and unmodeled jitter remain outside the stock-reference
exception. Hardware acceptance remains pending.
