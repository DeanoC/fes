# Managed DE25 board-SDRAM test

This development producer adds a `fes.ramtest` package for the managed DE25
computer profile. It destructively tests the **128 MiB board SDRAM**; it does not test either LPDDR bank. The screen says
`SDRAM 128M PASS`, `FAIL`, `RUNNING`, `WAIT` or `STOPPED`, and marks FPGA and HPS
LPDDR `NOT TESTED`. A quiet tone accompanies failure. This is a board-SDRAM test,
not whole-board acceptance or a replacement for the standalone three-bank
[JTAG diagnostic](../ramtest/README.md).

The existing scan engine performs eight write/read/byte-enable patterns and
latency samples through the board SDRAM controller. It starts fresh on each
normal computer Execution release and the controller is
ready. A Hold request stops the scan and is forwarded to the ordinary mailbox
only after accepted traffic drains. Missing memory responses keep Hold pending;
a timeout does not create drain. The existing kernel-owned display allocation,
F2SDRAM topology, and reset/epoch checks are unchanged. The package never
addresses HPS DDR as a tester. Its fixed-video/session-display route and
computer mailbox use the existing interface and programming profile.

```sh
make sim
```

This runs the real scan controller against a backpressured model, a real-mailbox burst-drain test, raster status test, and the actual three-clock standard shell
against the board-SDRAM pin model, including corruption and interruption. A shorter scan is selected only in
simulation; the producer selects the complete bank. The high-address pattern,
together with the address-XOR pattern, distinguishes every halfword address
bit; this is not a claim about all possible analogue or timing faults.
These tests do not prove fitting, timing, signal integrity, the LPDDR banks,
or physical kit behavior.

The registered producer is `../producers/build_ramtest_package.py`; it uses
Quartus Pro 26.3 and the existing managed machine timing, source identity and
package gates. A build must use clean committed source and an authenticated
vendor archive. An independent D12 twin, any seal, and exact-package J8G
acceptance remain separate evidence. No command in this directory programs a
kit.
