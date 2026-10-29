# First reset candidate: retained, not accepted for closure

Producer `6cb01a9123d20923032a3f47cb927409931b4af1` completed IP generation
with zero errors/118 warnings and compilation with zero errors/308 warnings.
Its SOF SHA-256 is
`d29f747aa52355a7ef24ceccd024e4337bf0fa9ad3ad001298ffb1d7c079d79d`.
The retained output is `out/de25-hps-reset-6cb01a9123/` in the DE25 bring-up
worktree. The [candidate manifest](candidate.json),
[preservation check](postcompile-preservation.json) and
[report hashes](full-report-sha256.txt) identify that result. Full input/build
receipts and path reports remain in that output, bound by the manifest's hashes.

All 23 constrained rows pass, with zero TNS. STA reports zero unconstrained
clocks, inputs and outputs. The duplicate clock and original RES-50001 findings
are absent, but this candidate introduces two high CDC-50012 findings, one
high RES-50002 and one medium CDC-50103 finding. The common raw reset feeds
two generated controllers that sample it through data inputs; vector-wide
forced synchronizer identification also splits recognition of the local chain.
Those findings require correction, rather than a waiver.

The separate slow-model path audit finds both registers and all 89 top
consumers. The retained stage setup/hold and release recovery/removal reports
contain real timed paths. They supplement the multicorner compiler result;
they do not resolve the reset-domain findings. All compiler report hashes
remained unchanged after this separate query.

The generic TMC-20012 rule still requests an output delay on LED[7], despite
the verified asynchronous LED output exception and zero unconstrained outputs.
No synchronous receiver or output delay is invented. RDC-50003 lists 16 chains.
MTBF analysis covers 25 of 43 identified chains and excludes 18; the modeled
value does not qualify the whole design.

The [software source binding](software-source-binding.json) retains passing
focused, affected-suite and consistency evidence for this candidate. No board,
FPGA, QSPI or block device was touched. This artifact remains a host diagnostic.
