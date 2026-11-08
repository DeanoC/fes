# USB-only boot stopped at runtime source verification

Operator `cb8ca5692`, compiled source `4f6694732`, boot `5cbd398052060507`.
The single allowed attempt is consumed; replay is forbidden. Recovery, one boot
FIT write/readback and fresh read, both separate live JTAG admissions, matched
cold-loader handoff and all nine entry/BSP startup checkpoints passed. Independent
cleanup passes exact USB permissions, recorded 15M readback, owned private-server
termination and free UART.

The program then reported `FES_NATIVE_USB FAIL native fabric: unsupported HPS
source context`. Its Go caller still used the generic verifier, which rejects
the native fes.computer profile. The build-side contained USB selection is
supported, but its runtime caller needs the corresponding read-only verifier.
USB source/pin observation, lease acquisition, driver open and Ready/Stop were
not reached. This result does not explain the earlier combined-image root-port
connection failure or prove physical USB behavior.

No core/ROM/data-release operation was selected. No further native command or
reprogramming followed failure. Halt/Park remain unproven. The continuing claim
stays held, spare card stays, factory restore remains waived and bottom kit was
untouched. Next is a narrow inspection verifier exposing identity/pin checks
without the underlying control methods, joined to the actual USB caller, then
rebuilt before another independently frozen bounded procedure.
