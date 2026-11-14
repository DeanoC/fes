# DE25 START response ownership

A START negotiation test exposed a boundary before ledger initialization:
after source preparation, `peek_header` accepted any header
while START negotiated. A late DATA reply could be consumed as pre-ledger
traffic before the synchronous configuration-control response.

The actual vendor parser reproduces this on firmware source
`c5c794535b7974eeb5d4a8b26ddc763e806b569a`: header `10000000` returns status
zero, advances the FIFO by two headers and reads no payload. The control uses
the final rejection test with the unchanged reference service substituted;
its failing output remains separate from the repaired run.

After preparation, the service now admits only the ATF client's synchronous
control job before initializing the new transfer ledger. The vendor parser
still checks the original command-specific status/payload bounds. Cold
notifications before preparation retain their existing parser phase. Failure
sets the existing sticky service error and retains source/controller state.
No reset, retry, register write, source release or new DATA permission is added.

Nine sanitized cases cover the legitimate control headers and late DATA,
reserved-job and foreign-client headers. The negative cases execute the actual
vendor parser and require rejection before any FIFO/payload consumption, no
SMMU or DATA activity, released parser locks and sticky rejection of another
attempt. Joined cold transfer, twelve warm generations/forty faults and the
source-release supplier also pass. This is software evidence, not a board test.

The preceding format-15 chain completes SDK, entry, full BL31, SPL/U-Boot,
composition and fresh native file admission. Its [matched control](matched-control.json)
is retained as a file-only control and will not be deployed: the START repair
changes the firmware producer. The repaired firmware, boot chain/composition
and fresh admission must be rebuilt and verified before a new bounded trial.
Unchanged SDK/entry artifacts may be reused only after every selected source
and artifact byte is checked against the repaired source.

No card, JTAG, kit or factory image was changed by this work. General
core-loading acceptance remains open, and PR #1791 remains `lane:deano`.
