# DE25 enclosing source-operation budget: 25 seconds

The experimental native source-operation budget is deliberately raised from
20 to 25 seconds. This covers the original pre-hash source clock through
transfer, final validation and the retirement/continuation checks using that
clock. No clock origin is moved or restarted. The separate preparation,
command, retirement, release, control, poll and host bounds are unchanged.
All ownership, mapping, integrity, retained-source and no-retry checks remain.

The firmware constant, sealed profile and native computer/MENU services agree
on the new limit; the trace decoder consumes the sealed-profile budget.
Cross-language regression checks reject drift. Firmware clock tests cover the
former 20.142473-second boundary, just below 25 seconds, expiry and overrun;
expiry remains sticky and cannot retry I/O. Native late-completion tests retain
configuration and reject control admission at the new deadline.

This is a policy change, not a claim that configuration became faster.
The [previous hardware failure](../2026-11-18-de25-final-cost-physical/README.md)
remains failed at 20,142,473 us under its original 20-second contract; its
procedure remains consumed. A fresh matched build, source/capture admission
and one-use bounded TRWU8CBL test are required for the new contract.

Focused supplier/clock/trace checks and native service tests pass; full affected
checks, matched build and new physical results are pending. No hardware action has yet
occurred for this source-budget change. Normal Kit D25 HIL, exact-head review,
full failure coverage and vendor reset/DMA guarantees remain open.
