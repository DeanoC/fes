# Owned soft stop and improved flash-read throughput

The2649a2ce6 operator first acknowledged one CLOSE for the previous timed-out
owned flash session, then began a fresh read from byte0. The450-second data
budget retired further READ grants. The Tcl producer acknowledged a unique CLOSE
while ownership remained established and exited with an error, as required for
an incomplete full read. No hard kill or resume was needed; pass2 never started.

The7,892,992 saved bytes in1927 complete reads independently match exact replies
and POST grants, and match the earlier4,239,360-byte prefix. This is not a complete
backup. Selective tracing improved observed throughput, but the full16MiB still
needs substantially longer than the model. The device-free44x result was not a
prediction of physical JTAG performance. No clock increase or data-path bypass
was introduced. The next fresh read uses a measured longer host-only allowance;
native boot, FIT/configuration arenas, RAM and core-load bounds remain unchanged.

Independent cleanup passed: no task namespace/server survives,15MHz/auto1,
exact USB permissions/private config bytes+mode600 and free UART are verified.
Both the prior uncertain session cleanup and this session's CLOSE are now
acknowledged. CPU Park was not reobserved. No flash/SDwrite, image programming,
reset or reboot occurred; factory media and the other kit remain preserved.
