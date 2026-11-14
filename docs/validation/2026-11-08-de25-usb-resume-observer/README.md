# Shared USB resume follows firmware completion

The paired host checker incorrectly required USB Resume to follow the frontend's
printed fetch diagnostic. The retained physical launch instead completed all
firmware opening records, resumed USB, then printed the fetch observation and
active App state. The native backend resumes inside `withInputPaused`; frontend
diagnostic output is a later observation, not firmware's ownership boundary.

The launch checker now anchors Resume to the last nonquiet firmware record in
the fully verified configuration/opening. It retains pause-before-mutation,
source/lease/generation, no-report-during-pause, completed-App ordering, fetch
identity/activity, all original time bounds and permanent retirement. Stop/quit
still require the first restored menu frame and final independent menu drain.

The primary synthetic model emits the observed resume-before-fetch sequence.
The diagnostic may also precede Resume if firmware is complete and completed
App publication still follows Resume. Both orders pass; premature resumption
and resumption after App completion reject.

[software-result.json](software-result.json) records the checks and source hashes.
The unchanged 50,223-byte startup and 10,278-byte launch response, exact compiled
contract projection and original partial receive/timing offers are committed in
the captured-launch fixture. The original checker fails this regression with
the physical rejection; the repaired checker passes. Ten capture mutations
still reject and prohibit another send. The original hardware attempt remains
failed, and this offline replay proves no return or shutdown.

The affected parent suite passes (three existing prerequisite skips), along with
planner, whitespace and generated-consumer consistency checks. Every selected
SDK, entry, BL31, U-Boot and composition input and all six native artifact hashes
remain unchanged. The existing `f8653867f` native chain can therefore be
re-admitted; no runtime or firmware rebuild is required for this host-only fix.

A separate bounded procedure must explicitly retain the prior interrupted
physical attempt on that compiled image. It must not reuse the consumed source
or reset the prior-attempt count. Full menu/core return, shutdown, physical
display/input and ordinary core-load acceptance remain open.
