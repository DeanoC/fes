# Bounded tape-cost and direct-reset trial

New boot4489a0cd784922b0 is matched to firmware source
ad38a355f7a7d3cea0a31ce2065b402952aca9a4. The source/build and all final software
checks are retained. Retired69a remains consumed1/max1 after the secure tape
limit; this changed firmware/cold/SPL/FIT chain is unused0/max1. Native runtime
and entry bytes reproduce the prior build. Transport failures remain1/max2.

The procedure preserves the existing top-only recovery, thirteen fixed network
uploads/write/readbacks, thirteen fresh reads, native source/CPU/memory and
keyboard/menu Ready gates. Its paired menu/ZX81/ZX80 sequence stops at the first
failure. If initial tape fails, cached containment and sampled callback costs
must precede the terminal marker. The diagnostic decoder joins their owner,
generation and counts to the raw console and matched native startup; actual
strong-C emitter fixtures cover valid, failed-reset and malformed records.
Those fixtures inject transport/time and do not establish board performance.

Startup100s, configuration20s, initial tape1s, launch100s, core close5s, menu
restore100s and final Park5s remain. Native450s/process480s, recovery270s,
upload320s and procedure840s/host890s remain. No uncertainty is retried. Both
programming phases require separate one-use fresh vendor scans. Top-only
private JTAG temporarily uses6M/restores15M; exact USB permissions, private
server exit and UART release are independently checked afterward.

The new staged programming, transport, full collector/lifecycle, upload,
receipt, tape and action-origin fixtures pass. Full native-file admission and
fresh-hash reuse pass; the actual timed worker prefix takes1.434s and performs
no HPS derivation. Six stale/foreign/incomplete-prefix cases reject before any
attempt or device access. Read-only USB identity was stable for60s. A new live
preflight and verified prepared archive still precede the single attempt.

Top TRWU8CBL remains claimed with its spare card installed; factory restore is
waived unless needed. Keyboard readiness was confirmed by the user. Bottom
TRWU8J8G is untouched. There is no new hardware result at preparation time.
A sampled profile or direct reset readback cannot promote a failed launch into
Running, drain, normal return or ordinary core-load acceptance. The goal of
DE10-equivalent core loading remains open.
