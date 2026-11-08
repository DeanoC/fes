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

[The machine-readable record](submission-deadline-software.json) binds source
file hashes, compiler environment and preserved failures. Parent checks and
native deployment of these changed runtime bytes are separate gates. The board
still runs the previously verified `c681f166` runtime. This repair cannot cancel
kernel/secure calls, guarantee scheduling during a SoC-wide stall, or protect a
lease after process death. It does not qualify physical failed-upload recovery
or change the default-off partial-persona policy in #1568.
