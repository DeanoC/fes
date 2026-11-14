# Paired service controller fixture

`menu-cycle.txt` is the unmodified first menu configuration/open/close segment
of `out/nano-mixed-usb-20261107/provider-recorded/case-0.log`, produced by
`tests/test_nano_mixed_cold.py` on source `31251509f93b8d51bee8b677defda79df9eb7a76`.
Its SHA-256 is `beae16cd64448e9e98be57e0f37a03e2814b62045bb02edeaadbe30d733ba6fd`
(5,690 bytes). The retained source/command/trace is under
`/home/deano/fes-retained/de25-shared-usb-menu-evidence-20261107-r1`.

The fixture executes the real strong cold C callbacks and vendor SHA with
injected registers/mailbox responses. Its menu close exercises the secure
callback's active-reader disable path. It contains no native Go App telemetry,
physical DDR/HDMI observation or board acceptance.

The paired controller tests combine its route/closure records with the existing
`nano-program-service/launch.txt` configuration queue capture, changing ownership,
generations and counters explicitly in a synthetic transport. App/menu JSON,
USB boundaries and catalogue/companion metadata in those tests are synthetic.
The existing menu-service tests separately exercise telemetry emitted by the
real Go service. Neither class of host test proves a paired physical launch.
