# First selected native USB hardware attempt: failed

TRWU8CBL executed one bounded USB-only test. The matched cold boot, strong secure
USB setup, nine CPU startup checkpoints, source context and all 328 sealed HPS
pin samples passed. USB Ready, hub/device enumeration, keyboard input, checked
USB stop and normal-kit acceptance did not pass. This is a failed diagnostic.

Observer/source commit: `b025abc26`. The actual native SDK, firmware and loader
were built from `aaa45acfc708ed8fa5b177d6daac1ebcaf88adaf`; FPGA producer
`7808f74c2` was unchanged. The source-bound chain is
`49679d09a2475e6bbeaa7c5f3e9b4778496017c08e9db0a7fc692486a99d8005`.
Observer-only changes were checked against every selected native source byte.
The candidate was not rebuilt to change a nonce or retry a failed proof.

One qualified recovery boot was followed by one Ethernet upload of the
2,682,768-byte FIT to `FES/UPLOAD.BIN`, its SD readback and a fresh independent
readback in the same owned session. The recovery FIT, manifest and raw prefix
were protected and verified. Then one volatile native programming and one
`fesboot` command started the USB test. The original factory card stayed safe
outside the board. No flash, recovery upgrade, native SD write or core load was
requested; runtime SD and FPGA controls were absent from this profile.

The first failure was:

```
dwc2: unsupported RX status 0; containment: dwc2: register wait exhausted address=0x500 value=0xc0128008 mask=0x80000000 want=0x0 elapsed=500.000942ms
```

The owner retained its resources and reported firmware status 4. The test stopped
on that first failure with no retry or further board command. A separate two
second receive-only UART capture found no queued bytes. Neither the trace nor
that empty tail proves a successful firmware USB stop or runtime halt. The
exclusive claim remains held; the board must not be described as USB-contained
or released. The failure is below the source/context/pin checks, in receive
handling and attempted channel retirement; its underlying cause is unproven.

The wrapper restored the original 15 MHz JTAG clock and exact USB permissions,
closed its UART and stopped its private top-only JTAG process. An independent
root inventory verified cleanup. The other kit and shared JTAG daemon were
untouched. Factory restoration remains waived by the user; no recovery command
was sent after the native failure.

Raw console/progress/receipt files, operator scripts and frozen preflight are
retained under `out/nano-usb-trial-20261104/`. Independent replay verifies the
matched boot/source/pin prefix, rejects the USB diagnostic and checks the single
upload/fresh-readback references and host cleanup. `result.json` contains the
source identities, trace hashes and exact evidence boundaries.

Preparation checks passed: 71 observer/context/pin tests, 15 actual upload-runner
model cases, 54 receipt mutations, and execution of the actual native operator's
decoder/acceptance expressions on success, a bare loader prompt and five failure
cases. The observer accepts the pinned runtime's clean halt once after USB Pass;
premature, duplicate and nonzero halts reject. An initial missing-prefix rejection
was caught by the new halt regression and corrected before this hardware attempt.

The follow-up driver prototype adds an RX fault snapshot and tests zero/reserved
statuses with the observed stuck channel. It preserves the first peek/pop values,
returns no input, keeps containment timeouts visible and permits no subsequent
hardware access through the retired host. It changes no status acceptance rule.
The snapshot has model evidence only and needs a new matched boot chain before
another bounded hardware diagnostic. The failed image will not be retried.
