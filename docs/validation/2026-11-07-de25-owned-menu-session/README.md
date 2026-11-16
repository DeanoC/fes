# Native menu memory and owned firmware return

Source `7523333b1ac505f924604232e775a950dc17670f` binds the private menu presenter to its own compiled two-slot 8 MiB reservation, completed source transfer and owned firmware bridge lifecycle. A successful return independently checks master stop, bridge drain/closure, source identity and the complete retained arena. Cancellation and uncertain writes cannot produce a normal return or free either slot.

All 12 affected checks pass. Native session tests inject secure responses; 33 presenter/stop cases use actual menu CSR/video/256-bit DDR RTL and the actual C stop helper. The complete menu cold-provider objects compile with their configuration dispatcher. ARM64 native/board archives compile, including an explicit 8 MiB board-layout fixture; that fixture is not a sealed boot profile.

No board image selects this path yet. The joined menu/secure-provider execution, sealed FPGA producer, pixel-clock/reset/HDMI wiring and menu storage resume/reconfiguration remain open. These results are host evidence; no hardware was touched and no physical DDR, HDMI, timing or ordinary menu admission is claimed.

The next integration is the sealed board producer and matching native boot selection. The full DE10-style core-loading goal remains active. See [the machine-readable results](result.json) for exact inputs and limits.
