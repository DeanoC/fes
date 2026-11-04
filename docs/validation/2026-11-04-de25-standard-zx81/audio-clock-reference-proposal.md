# Proposed stock-reference treatment for two audio clock outputs

Decision requested: extend the existing Terasic-reference output review to
**HDMI_SCLK and HDMI_LRCLK only**, on the standard internal16K ZX81 base.
The current exporter remains blocked; this proposal has not changed admission.

The exact source446 full build passes all92 numerical timing checks. The fitted
review checks five clock definitions,32 routes across four operating conditions,
all five audio pins/voltage settings, the complete stock PLL with device-only
retargeting, and the existing transmitter configuration. The real serializer
regression proves LRCLK changes at falling SCLK; the transmitter captures at
rising SCLK. Evidence is in [audio-clock-fitted-review.json](audio-clock-fitted-review.json)
and [canonical-phase-candidate.json](canonical-phase-candidate.json).

If approved, the package producer would require all of the following:

- The standard `de25-linux-computer-v1` internal16K base with expansion disabled,
  and the reviewed constant-PCM audio implementation. Changes to its serializer,
  wrapper or receiver mode require another review rather than automatic acceptance.
- Exact stock pins, I/O standards, PLL parameters and transmitter writes, including
  rising-edge audio capture. The frequency-only clock model is refused.
- Correct internal and forwarded clock phases, and complete min/max clock-network
  and output-route coverage across all four fitted operating conditions, bound to
  the selected source, compiled database and RBF.
- The exact output-finding set: the existing30 reference outputs plus these two
  clock outputs, with no additional port, reason or waiver. Raw findings remain
  retained; no fictitious output delay or false path is added.
- Every ordinary numerical timing check, the already approved two-input review,
  independent byte-identical package rebuild, and exact-package board acceptance.

This accepts a stock-reference engineering basis for the two missing-output-delay
findings. It **does not** establish independent PCB timing: no trace-delay figures
are available, and the reported conditional budgets exclude PCB differential
skew, unaccounted clock jitter/uncertainty and signal-integrity behavior. It grants
no permission to ignore other timing findings or to promote a hardware baseline.
