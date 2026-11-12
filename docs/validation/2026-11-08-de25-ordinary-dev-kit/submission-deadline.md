# DE25 configuration submission deadline

The programmer at `086fa0bcd` checked its deadline after synchronous configfs
write/close. A blocked kernel callback therefore prevented the caller returning
by that deadline. A private reproduction using the actual programmer, modeled
configfs and a real monotonic clock holds close beyond a 200 ms deadline. The
caller is still pending at 400 ms. Releasing the fixture eventually produces
the existing uncertain error and replay refusal. This is software fault
injection, not a physical failed-upload result.

The repair moves exactly one write and close into an owned worker. Its immutable
overlay bytes, descriptor and duplicate existing physical flock outlive the
manager. Timeout returns failure and retains the existing poisoned context,
firmware and search override. It neither retries nor resets hardware. Only
completed submission permits status checks and normal success. An independently
opened competing flock remains refused after manager destruction while the
callback is blocked, then succeeds after actual callback/worker retirement.

Real-clock tests cover blocked write and close, success, short write, close
error, a far-future deadline and refused replay. The first AArch64 run timed
out; a trace of that exact binary shows the fourth fixture spinning in clock
conversion. The target C++ library converts steady condition-variable waits
through realtime, which overflows for that distant deadline. Short steady waits
bounded by the original deadline fix the conversion. The corrected AArch64-QEMU
suite passes with the matched SDK; it remains CPU software evidence. Native
tests and ASAN/UBSAN pass. An earlier native invocation used the wrong working
directory and failed opening its existing relative fixture; that failed record
is retained alongside the corrected component-directory run.

[The machine-readable software record](submission-deadline-software.json) binds
source file hashes, compiler environment and preserved failures. The subsequent
[native record](submission-deadline-native.json) tests the actual default-off
bundle `ca85c2fd`, runtime `a495366c`, built from `644ed8017` against the retained
normal SDK/root. One supported install and generation-CAS activation selects it
at generation6. Every selected file and all three running executables are
authenticated. MENU remains ready for ten seconds; the same private host and
catalog reconnect without restart. Two fresh empty ROM prompts precede the first
key in one ordinary ZX81 launch. LOAD7, edit/run9, reload7 and Stop/MENU pass with
six inspected black-on-white captures across candidate and restoration controls.

One supported activation restores the original `61a4b0aa` bundle at generation7,
followed by the same ordinary 7/9/7 control. Exact durable accepted receipts,
replaced processes, immutable kernel/root/stock FIT and original data/catalog,
pairing and saved923-byte program are verified. The board is released at ready
MENU, idle and lease-free. No card swap, power cycle, OS reboot, image/kernel/FIT,
QSPI or persona operation occurs. No main baseline is promoted. The changed
runtime inherits no earlier runtime-loss/reboot fault qualification.

The board again runs the previously verified `c681f166` runtime. This repair cannot cancel
kernel/secure calls, guarantee scheduling during a SoC-wide stall, or protect a
lease after process death. It does not qualify physical failed-upload recovery
or change the default-off partial-persona policy in #1568.

Integration checks pass `make check`, 1,423 parent regressions with one skip,
the selected host functional/race and UI checks, and the runtime functional
loop through all 29 binaries. The broad coordinator is terminated with143
during the subsequent exhaustive header-rebuild check; no final success receipt
is written and its cause is unestablished. That interruption leaves seven
touched header timestamps. The first independent dependency check correctly
refuses a stale binary; the exact pre-test timestamps are then restored from
the owned test snapshots. The corrected dependency check, version rebuild,
archive/architecture checks and support-truth checks all pass. The last runtime
binary is also rerun alone to confirm its otherwise unobserved terminal exit0.
The original failed/interrupted records remain retained. This is separate
completed check coverage, not a green result for the interrupted full runner
or its uncompleted exhaustive recompile. [Integration receipt](submission-deadline-integration.json).
