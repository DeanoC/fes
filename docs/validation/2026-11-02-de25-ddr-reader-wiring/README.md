# DE25 bounded DDR reader wiring and host validation

The prototype now has an explicit `hps-ddr` backend connecting the real
control/drain endpoint to the generated HPS memory adapter. The default
on-chip responder remains an independent diagnostic backend. This record
qualifies generated wiring and host models only; no board access, bridge
release, SOF/RBF compilation, package seal or physical DDR test occurred.

The final source is `17fb4b05cdc4998f4bd71a26358936c56444689f`, based on `180eac38d`. The [code](../../../sources/misteross/prototypes/de25_board_control/README.md)
describes the interface and build options. Ownership remains `lane:deano`.
The [preceding physical result](../2026-11-02-de25-reader-native-cycle/README.md)
belongs to its exact older on-chip artifacts and is not inherited by this producer.
TRWU8CBL was left untouched in that completed native diagnostic.

The DDR backend validates every staged request against a compiled bus window,
16-byte alignment and a single 4 KiB page. It snapshots the request and permits
one outstanding diagnostic burst. Offered AXI addresses survive backpressure
and disable. Actual RID/RRESP/RLAST and reader backpressure feed the existing
generation/cookie drain endpoint. Debug byte/checksum/data registers grant no
drain authority. All required write request pins are hardwired inactive, and
unexpected write responses fault the reader. The default on-chip mode cannot
issue DDR requests and ignores external response inputs.

Backend and window enter the diagnostic fingerprint and canonical functional
record. DDR uses distinct control ABI `0x10203041` and distinct diagnostic
programming profiles; on-chip uses `0x10203040`. No physical runtime profile
was registered. The native cold/serial provider retains its original data-held
policy and on-chip reader scope. Its context-builder input inventory now also
captures the new reader-selection helper.

Both final-source projects were freshly generated with Quartus Pro 26.3 and
the same frozen board source/vendor archive. The DDR project uses bus window
`0x1000..0x3000` **only as an off-board fixture**. It supplies no CPU mapping,
live reservation, cache policy or authority to open a data bridge. The actual
generated interface, source bytes, forwarded HDL parameters, 100 MHz clock,
reset, peripheral/JTAG maps and HPS DDR route passed the checks in the
[DDR generation](generation-ddr.json) and [on-chip generation](generation-onchip.json)
receipts. Individual vendor calls retained their 120-second cap.

Actual generation exposed two integration requirements: Platform Designer
requires a full AXI pin set even when writes are inactive, and adding a
four-bit manager ID exceeded the existing HPS adapter's routing capacity.
The fixed zero ID now uses one wire; the vendor adapter width is unchanged.
The observer follows Quartus's actual literal wrapper defaults, exact named
forwarding and non-overriding parent instance. Quartus does not emit the
initially assumed top-level handoff parameter fields. The first failed
projections and the stopped observer/mutation fixtures remain retained.

The [DDR model](model-ddr.json) passed 23 transfer/error cases and three
identity/window rejection cases against the final project's actual sources
and build ID. Tests cover held addresses, response stalls, disable while a
response is held, partial drain, exact data, maximum bursts, all bounds,
protocol errors, unsolicited read/write responses and permanently inactive
write requests. The [on-chip project model](model-onchip.json) passed its
control, Tcl and native Go adapter checks, plus the DDR branch check.
The final [DDR handoff mutations](handoff-ddr.json) rejected all 11 altered
copies; the [on-chip mutations](handoff-onchip.json) rejected all four.
Wrong routes, widths, spans, source bytes, window forwarding, parent overrides
and an enabled write pin cannot pass.

All eight [affected software groups](software.json) passed on
`0775d3fcfaa1c9469e4cdfa8bb58e64944bc0b59`: 1265 parent tests
(two skips), 1576 FPGA software tests (75 skips), generated
consistency and the control/native integration checks. That software head is
preserved separately from the final source. The final [scope audit](source-delta.json)
proves that its only follow-up changes are the emitted-parameter observer and
its mutation-test function; all artifact-emission functions, RTL and runtime
sources are unchanged. Focused software checks, fresh generation, actual
handoff mutations and both project models validate that follow-up. The eight
groups were not rerun or relabelled as executions on the final head. The
unchanged provider/RTL inputs also passed their eight joined success/failure
cases during development.

The remaining gate is a trusted native DDR reservation/mapping and a complete
master inventory with real stop/drain callbacks, followed by a matched boot
chain, bounded physical read/drain and runtime package admission. Inherited
FPGA masters, cache/lifetime ownership, bridge shutdown, repeated core loads,
physical launch/return/failures and peripheral/UI acceptance remain open.
An on-chip drain, generated route or host model does not satisfy those gates.

Source, every generated project, model executable, selected software logs,
negative fixtures and development failures are retained with independently
verified hashes under `/home/deano/fes-retained/de25-ddr-reader-20261102`.
