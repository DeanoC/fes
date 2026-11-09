# Bounded secure-mailbox hardware trial

The compiled candidate is `6c9e1aaecb129336`, from source
`bd876a9bc530e11a870facb35a7588457337db2a`. The preceding
`0398905b34433547` trial demonstrated expected mailbox registers before MMU
activation and through its secure alias afterward, while the original NS
mapping read zeros and the unchanged driver timed out. Its operations remain
consumed and retired. Secure command acceptance is still unproven.

This candidate selects physical mailbox access before MMU activation and the
existing private secure alias afterward. Only that page's read-only bit changes;
all other descriptors, layout and protected CPU/ownership/master/control/tape
sources remain identical. Original vendor transport/service C bytes, commands,
arguments, response rules, resets, retries and deadlines remain unchanged.
The matched build, real driver tests in both CPU states, compiled selector,
emulated execution and selected integration results are retained in the
candidate evidence. Those results do not establish physical command acceptance.

Use only the continuing exclusive claim on top TRWU8CBL. The spare test card
stays installed, its factory card remains safe, and the user's factory-restore
waiver continues. Do not access bottom TRWU8J8G. No physical input action is
requested. Require the current admitted file closure, committed operator
sources, more than 30 GiB free, unique stable USB identity, free UART and a
verified prepared archive before programming.

Take the shared Quartus lock as deano before sudo. Use the separately qualified
fixed 3 MHz/auto-adjust 0 transport, retaining both historical programmer
failures. Recovery and native programming each require a fresh durable one-use
reservation and scan. Never replay a consumed operation or image. Require all
13 network uploads and fresh SD readbacks before native programming. Require
the complete original native boot proof before the seven-configuration,
three-launch, four-menu and tape exercise. Preserve native 450 s, outer 800 s,
host 890 s and tape 1 s bounds. Stop on the first failure with only the original
two-second passive tail; preserve its evidence rather than retrying.

After the terminal result independently verify restoration of original
15 MHz/auto-adjust 1, exact USB permissions, removal of only our private server,
and a free UART. Retain the complete terminal archive and source bundle, append
the actual result to the owned kit log, and distinguish upload/readback, boot,
FPGA configuration, full cycle and ordinary appliance acceptance. A successful
bounded diagnostic alone does not establish full DE10-equivalent readiness.
