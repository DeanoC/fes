# Bounded service-claim recovery candidate

Base FES source: `c6e978159dcf5277c0c86dbd0f368ff036b314b5`.
Scope: a patch for the exact locked Terasic kernel file and a source-level proof.
No image, running kernel, FPGA, runtime ABI or shared definition changes.

The [candidate](../../../image/kernel/de25-fpga/README.md) passes nine tests of
the actual patched function with simulated firmware replies and time. The
unmodified source hangs on the identical permanent-BUSY control; its owned child
is stopped by the test's one-second deadline. The successful four-buffer callback
is retained, real progress refreshes only the existing inactivity deadline, and
cancellation sends no stale callback.

The complete modified `stratix10-svc.c` also compiles against the real pinned
kernel source/configuration as an AArch64 kernel object. This compilation uses
the pinned builder `sha256:ea200655b92ab1211bc8b326c6e799815f1d648b2d5e5a6c982f597cb2b9650d`
and FES AArch64 SDK archive
`526aa1624677f77ace7057112f9c9f5ac8afcf79ba2c78161fe1a43c6dea2bd1`
from the retained Linux producer `24fc445a2`. The compiler reports Buildroot
2021.02.4 GCC 9.4.0. The object is AArch64 ELF and hashes to
`d1560afc49d3ae096396ee9d015bd32e3fdaba2e40a7efdf2351b6ff972f9e9a`.
The object build reports no compiler warnings or errors.

This is host behavior plus a real AArch64 kernel compilation. It is not a boot
test or physical failed-load recovery acceptance. The underlying transfer timeout
is unresolved. Before installation, prepare a distinct complete diagnostic kernel
and its matching modules, preserve the approved rollback baseline, and verify
bounded failure cleanup on the actual board. Do not represent this patch as
successful Linux core loading or close [#1298](https://github.com/DeanoC/fes/issues/1298).

Bounded error return is separate from hardware quiescence. This candidate does
not establish that an outstanding SDM transfer has stopped; retain its buffers
and require a verified reset boundary before retry/unload/reuse.
