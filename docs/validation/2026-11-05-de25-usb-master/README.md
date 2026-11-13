# USB PIO retained through owned master stop

Source `7ba6024e9` adds an explicit trusted USB master policy to the existing
boot/bridge engine. It binds once to the exact accepted cold bridge object,
before any device release, and retains only the paired USB0 reset bits. The
existing cold all-reset stop API remains strict. No non-secure initializer,
generated wire capability or composed boot selector is enabled by this change.

The new policy requires the existing cold composition, confirmed USB ownership,
DMA and interrupts disabled, live platform readiness, and an independently
observed idle USB interconnect master. Its poll is bounded. Owner, epoch, USB
generation and retained reset mask must remain consistent through callbacks.
It delegates FPGA quiesce/quiet to the independently supplied producer callbacks;
USB readiness cannot supply a fabric-stop acknowledgement.

The bridge engine excludes only the verified USB pair from its reset assertions.
It still checks all other master resets and the full producer bitmap at the
stop publication boundary. Copied callback descriptions retain their original
context; a foreign owner/epoch, duplicate binding or unsupported reset mask
cannot grant bridge authority. The secure platform exposes a binding hook under
its existing lock; the normal startup does not call it yet.

The actual C boot/master, bridge and SD services run three cycles with injected
registers. USB remains confirmed while the SD service resumes, prepares and
stops, and while control-only or DDR bridge paths open and close. The tests
check that USB's active channel, write count and generation remain unchanged,
and that the master engine issues no USB reset. The DDR close reports the exact
drained bit; control-only closes retain their held-data provenance.

SD resumption follows the existing qualified return policy. The two reusable
SD/core cycles use held-data returns; the final DDR close does not resume SD
without its separately required reader/window return proof. An early draft
attempted that unqualified resumption and was correctly rejected; the test
sequence was corrected without weakening the SD service.

Failure cases cover DMA, a busy USB master, missing/failed producer stops, late
loss of the producer bitmap, a USB generation change during stop, unsupported
producer bits and missing explicit master binding. Failed closes leave output
receipts untouched, retain USB's active reservation and perform no retry.
Private negative controls remove the live-idle poll and final producer check;
the corresponding rejection tests must then fail.

The initial candidate `1d8fa4df6` placed the stricter late-stop condition in
general containment. The actual strong-firmware rearm test rejected repeated
loads: a successful new configuration legitimately replaces the previous
fabric-stop inventory while data remains held. Only this task's validation jobs
were stopped after that observed failure. The repair limits the extra complete
producer sample to the stop publication boundary. Both the injected late-loss
test and the actual configuration/reload cases pass with the repaired source.
The failed candidate, cancellation records and diagnostics are retained.

Validation is recorded in `result.json`, including individual bounded software
phases and the native/BL31 cross-build. Hardware programming, card writes and
physical combined USB/core qualification are outside this result. The actual
build remains USB-only and does not invoke the new composition/master binding.

Source bundles, complete per-phase logs, failed-candidate diagnostics, private
negative controls and rebuilt artifacts are retained under
`/home/deano/fes-retained/de25-usb-master-20261105-r1/`.
`retention.json` records file-by-file archive readback and restoration of the
source with its prerequisite bundles plus strict Git fsck. External toolchains
remain pinned dependencies. No working artifacts were removed.

Next, update the owned USB service, dispatcher and generated capability contract
together, then select and wire the native input owner into a matched composed
boot chain. Normal package/ROM/media admission, physical core execution/video,
and sustained normal-kit operation remain required for DE10-style readiness.
