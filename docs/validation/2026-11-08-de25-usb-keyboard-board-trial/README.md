# Prepared explicit keyboard Ready/Stop trial

One new attempt on TRWU8CBL selects `boot-keyboard` explicitly. Compiled source
`34da5c71f1b9db5c6fff94dd4e8fbfe5aaa4c6b1`, native boot
`f86477977fa75f72`. This is a different image/profile from the consumed gamepad
trial `3c23bd7f67779909`, which correctly rejected System/Consumer Control.
No gamepad admission, automatic protocol fallback or input-policy weakening is
selected. The physical peripheral models are still being clarified; this test
can establish keyboard presence/readiness or retain captured topology IDs on
failure. No hardware result is claimed by this preparation record.

The matched SDK, entry, firmware and cold profile agree on keyboard input.
Full native/source validation and complete cached-file re-admission pass. Six
stale/forged worker-prefix cases reject before an attempt or device access.
Actual programming functions and boot-only UploadSession pass injected tests.
Keyboard capture joins reject missing startup/Ready/Stop, replay, failure,
wrong owner/generation/build/selection and forged result booleans.

The one-attempt procedure uses one qualified recovery, one fixed boot FIT
upload/write/readback and a fresh read, then separately scanned native image
programming and one checked `fesboot`. The decoder requires all nine entry
checkpoints, source/pins, typed keyboard Ready, Stop generation 3 and Pass.
It requires no button press or report; physical input is separate evidence.
It selects no ROM, core command, FPGA programming service or data release.

Unchanged limits: register wait 500 ms, enumeration four seconds, caller
startup five seconds, observation 60 seconds, native 180/worker 210 seconds,
recovery/upload/procedure/outer 270/320/840/890 seconds. First failure is
terminal and no replay is allowed. Top-only private JTAG and per-image one-use
live scans, current continuing claim, exact USB/UART admission, temporary 6M
and restored 15M, exact permissions and owned-server exit remain required.
The spare stays, factory restore is waived unless needed, and bottom TRWU8J8G
is untouched. This diagnostic cannot establish ordinary core-load acceptance.
