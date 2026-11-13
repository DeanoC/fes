# Preserve the original native stop failure

A local menu-stop failure can be followed by successful independent firmware
closure. The service correctly remains retired, but the next menu presentation
previously returned only the generic retired sentinel. Frontend retirement then
replaced the useful cached error in the final panic. The historical warm-return
trace therefore cannot establish the original native cause.

Later presentation, service requests and frontend retirement now include the
original cached status error. They retain `errors.Is(err, ErrProgramRetired)`
and perform no device operation. Healthy launch/return, stop bounds, independent
firmware closure, retained ownership and no-retry policy remain unchanged.

The regression uses the real menu/service/frontend path with injected CSR and
firmware suppliers. An uncertain local stop is independently contained once,
then presentation, Park and a later frontend error must all preserve its cause.
It checks zero further device accesses, no release and no SD resumption.
The test fails on the old code at presentation, then passes with the repair.

The [result](result.json) binds source hashes and supervised checks. The full
native Go module race suite passes in 139 seconds; generated/source checks
pass. The complete selected-program TamaGo SDK also cross-builds. A newly
matched entry/BL31/SPL/U-Boot/FPGA composition is still required for these
changed native source bytes. The earlier matched candidate remains separately
bound to its previous source, with no physical acceptance. The historical
warm-return attempt remains failed and its root cause remains unproven until
a fresh bounded trial captures the preserved error or verifies the repair path.
