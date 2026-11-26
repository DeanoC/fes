"""DE25 derivations of the unchanged DE10 pc386 machine sources.

The DE10 machine carries its own session-display plane (fes_display_plane
behind pc386_plane_bridge), which reads the launcher slots from window offset
0 on DDR port 2. A DE25 machine shows its picture through the board's
fes_de25_session_display instead, which owns the scanout's own grant. So the
plane comes out of pc386_machine: video is the presenter's 720p word, the
plane's port-2 requester is idle, and pc386_system carries the mailbox's
display opcodes out to the board in place of into the machine.

The DE25 package exposes only the ATA disk. Keep its unit-zero storage
endpoint and hard-disk boot, with no floppy drives fitted.

Controller 2 of the AT core's DMA is its register file only
(pc386_dma2_registers.sv): it reads back as fes_dma8237 does, but runs no
cycle, so a software request on channels 4-7 does not count down or set TC.
Nothing in the machine raises a controller-2 DREQ. The DE10 keeps the full
controller.

For the 486DX model, retarget_system_cpu then points pc386_system's CPU at the
DE25's i486DX, fes_i486dx_cpu (i486dx/derive.py); the 486SX keeps fes_i386_cpu.

Each derivation fails if its anchor moves. A removed block is pinned by its
SHA-256, so a change inside it fails too.
"""
import hashlib

PLANE_START = b"    wire plane_hold, plane_ddr_reset"
PLANE_END = b"    pc386_port2 port2 (\n"
PLANE_SHA256 = "82cf4a29b9c26d4a57803219896a57407a4cf41394451ecbbc602f67787c982a"
PLANE_AFTER = (b"    // DE25: the board's fes_de25_session_display blends the launcher over\n"
               b"    // this word, so the machine's video is the presenter's, and the plane's\n"
               b"    // port-2 requester is idle.\n"
               b"    assign video = presenter_video;\n"
               b"    assign plane_underflows = 32'd0;\n"
               b"    assign {display_response_valid, display_response_error, display_response_data} = 18'd0;\n"
               b"    assign display_quiesced = 1'b1;\n"
               b"    wire [28:0] bridge_address = 29'd0;\n"
               b"    wire [7:0] bridge_burstcount = 8'd0;\n"
               b"    wire bridge_read = 1'b0;\n"
               b"    wire bridge_waitrequest, bridge_readdatavalid;\n"
               b"\n")
MACHINE = ((PLANE_START, PLANE_END, PLANE_SHA256, PLANE_AFTER),)

SYSTEM_WIRES_BEFORE = (b"    wire display_request, display_response_valid, display_response_error, display_quiesced;\n"
                       b"    wire [6:0] display_opcode;\n"
                       b"    wire [7:0] display_index;\n"
                       b"    wire [15:0] display_argument, display_response_data;\n")
SYSTEM_PORTS_BEFORE = (b"    // The plane's output, in pixel_clk\n"
                       b"    output wire [27:0] video,\n")
SYSTEM_PORTS_AFTER = (b"    // The presenter's 720p word, in pixel_clk; the board blends the launcher\n"
                      b"    // over it (fes_de25_session_display)\n"
                      b"    output wire [27:0] video,\n"
                      b"    // The mailbox's session-display opcodes, to the board's display\n"
                      b"    output wire display_request,\n"
                      b"    output wire [6:0] display_opcode,\n"
                      b"    output wire [7:0] display_index,\n"
                      b"    output wire [15:0] display_argument,\n"
                      b"    input wire display_response_valid, display_response_error,\n"
                      b"    input wire [15:0] display_response_data,\n"
                      b"    input wire display_quiesced,\n")
SYSTEM_MACHINE_BEFORE = (b"        .display_request(display_request), .display_opcode(display_opcode), .display_index(display_index),\n"
                         b"        .display_argument(display_argument), .display_response_valid(display_response_valid),\n"
                         b"        .display_response_error(display_response_error), .display_response_data(display_response_data),\n"
                         b"        .display_quiesced(display_quiesced), .video(video),\n")
SYSTEM_MACHINE_AFTER = (b"        .display_request(1'b0), .display_opcode(7'd0), .display_index(8'd0),\n"
                        b"        /* verilator lint_off PINCONNECTEMPTY */\n"
                        b"        .display_argument(16'd0), .display_response_valid(),\n"
                        b"        .display_response_error(), .display_response_data(),\n"
                        b"        .display_quiesced(),\n"
                        b"        /* verilator lint_on PINCONNECTEMPTY */\n"
                        b"        .video(video),\n")
# With no plane its underflow count is zero, so the counter snapshot takes only
# the two live counts: a constant field would split its copy and snapshot
# registers into auto-named parts, and the board's constraints name them.
SYSTEM_SNAPSHOT_BEFORE = (b"    pc386_snapshot #(.W(96)) pixel_counts (\n"
                          b"        .source_clk(pixel_clk), .value({frames_shown, plane_underflows, present_underflows}),\n"
                          b"        .clk(clk), .snapshot({frames_shown_sys, plane_underflows_sys, present_underflows_sys})\n"
                          b"    );\n")
SYSTEM_SNAPSHOT_AFTER = (b"    pc386_snapshot #(.W(64)) pixel_counts (\n"
                         b"        .source_clk(pixel_clk), .value({frames_shown, present_underflows}),\n"
                         b"        .clk(clk), .snapshot({frames_shown_sys, present_underflows_sys})\n"
                         b"    );\n"
                         b"    assign plane_underflows_sys = 32'd0;\n")
SYSTEM_FLOPPY_CAPABILITY = (b".ENABLE_WALL_CLOCK(1),\n"
                            b"        .ENABLE_PC_FLOPPY(1)\n")
SYSTEM_FLOPPY_TYPES = (b"    // The floppy drives (docs/design/pc386-removable-media.md, slice F3, as\n"
                       b'    // decided on #1694): A: is a 3.5" 1.44M drive and B: a 5.25" 1.2M one\n'
                       b"    // (CMOS types 4 and 2), and the machine boots like an AT, A: then C:\n"
                       b"    // (boot device 1: CMOS 3Dh=21h, 38h=30h, 2Dh bit 5).\n"
                       b"    localparam [5:0] FLOPPY_TYPES = {3'd2, 3'd4};   // {B:, A:}\n")
SYSTEM_FLOPPY_PORTS = (b"        .floppy_type(FLOPPY_TYPES), .boot_device(2'd1),\n"
                       b"        .floppy_present(floppy_present), .floppy_write_protect(floppy_write_protect),\n"
                       b"        .floppy_two_sided(floppy_two_sided), .floppy_cylinders(floppy_cylinders), .floppy_spt(floppy_spt),\n"
                       b"        .fdc_frozen(fdc_frozen), .fdc_busy(fdc_busy), .fdc_fenced(fdc_fenced), .fdc_written(fdc_written),\n"
                       b"        .floppy_verify(floppy_verify), .floppy_sectors(floppy_sectors), .floppy_crc(floppy_crc),\n"
                       b"        .floppy_verify_done(floppy_verify_done), .floppy_verify_match(floppy_verify_match),\n")
SYSTEM_ATA_ONLY_PORTS = (b"        .floppy_type(6'd0), .boot_device(2'd0),\n"
                         b"        .floppy_present(2'd0), .floppy_write_protect(2'd0),\n"
                         b"        .floppy_two_sided(2'd0), .floppy_cylinders(14'd0), .floppy_spt(12'd0),\n"
                         b"        /* verilator lint_off PINCONNECTEMPTY */\n"
                         b"        .fdc_frozen(1'b0), .fdc_busy(fdc_busy), .fdc_fenced(fdc_fenced), .fdc_written(),\n"
                         b"        .floppy_verify(2'd0), .floppy_sectors(26'd0), .floppy_crc(64'd0),\n"
                         b"        .floppy_verify_done(), .floppy_verify_match(),\n"
                         b"        /* verilator lint_on PINCONNECTEMPTY */\n")
SYSTEM_STORAGE_START = b"    // The floppy drives A: and B: (fes.media.pc-floppy, units 1 and 2) take\n"
SYSTEM_STORAGE_END = (b"        .exec_reset(exec_reset), .execution_accepted(execution_accepted),\n"
                      b"        .disk_state")
SYSTEM_STORAGE_SHA256 = "c83af7ac25b76ee8f26c56d328f87fa665256b555814dab0b33734bd6e718051"
SYSTEM_STORAGE_AFTER = (b"    fes_computer_storage #(.ENABLE_ATA_DISK(1), .ENABLE_DATA(1), .ENABLE_WALL_CLOCK(1)) storage (\n"
                        b"        .clk(clk), .request(storage_request), .opcode(storage_opcode), .index(storage_index),\n"
                        b"        .argument(storage_argument), .response_valid(storage_response_valid),\n"
                        b"        .response_error(storage_response_error), .response_data(storage_response_data),\n")
SYSTEM = ((SYSTEM_WIRES_BEFORE, b"", 1), (SYSTEM_PORTS_BEFORE, SYSTEM_PORTS_AFTER, 1),
          (SYSTEM_MACHINE_BEFORE, SYSTEM_MACHINE_AFTER, 1), (SYSTEM_SNAPSHOT_BEFORE, SYSTEM_SNAPSHOT_AFTER, 1),
          (SYSTEM_FLOPPY_CAPABILITY, b".ENABLE_WALL_CLOCK(1)\n", 1),
          (SYSTEM_FLOPPY_TYPES, b"    wire fdc_busy, fdc_fenced;\n", 1),
          (SYSTEM_FLOPPY_PORTS, SYSTEM_ATA_ONLY_PORTS, 1),
          (b"        ddr_hold <= drained && !verifying && !(|floppy_verify);\n",
           b"        ddr_hold <= drained && !verifying;\n", 1))

AT_DMA2_BEFORE = (
    b"    fes_dma8237 #(.ADDRESS_BITS(24), .AT_PAGES(1), .REFRESH(0), .WORD_ADDRESS(1)) dma2 (\n"
    b"        .clk(clk), .reset(reset), .guest_ce(dma_tick),\n"
    b"        .reg_read(access && !cycle_write && select_dma2), .reg_write(access && cycle_write && select_dma2),\n"
    b"        .page_write(page_write && page_second),\n"
    b"        .reg_address(select_page ? {2'b00, page_channel} : port[4:1]), .reg_wdata(cycle_wdata[7:0]),\n"
    b"        .reg_rdata(dma2_data),\n"
    b"        .drq(3'b000), .refresh_clock(1'b0), .hold_request(dma2_hold), .granted(dma2_hold),\n"
    b"        .cpu_waiting(1'b0), .cpu_done(1'b0),\n"
    b"        .cycle_valid(dma2_valid), .cycle_ready(1'b1), .cycle_done(dma2_done),\n"
    b"        .cycle_address(), .cycle_wdata(), .cycle_kind(), .cycle_channel(), .cycle_write(), .cycle_tc(),\n"
    b"        .cycle_rdata(8'hff), .cycle_fault(2'b00), .fault(), .refresh_done()\n"
    b"    );\n")
AT_DMA2_AFTER = (
    b"    // DE25: controller 2 is its register file only (pc386_dma2_registers).\n"
    b"    pc386_dma2_registers dma2 (\n"
    b"        .clk(clk), .reset(reset),\n"
    b"        .reg_read(access && !cycle_write && select_dma2), .reg_write(access && cycle_write && select_dma2),\n"
    b"        .reg_address(port[4:1]), .reg_wdata(cycle_wdata[7:0]), .reg_rdata(dma2_data)\n"
    b"    );\n"
    b"    assign dma2_hold = 1'b0;\n"
    b"    assign dma2_valid = 1'b0;\n")
AT_CORE = ((AT_DMA2_BEFORE, AT_DMA2_AFTER, 1),)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def _record(source, result, changes):
    return {"source_sha256": sha(source), "derived_sha256": sha(result), "changes": changes}


def retarget_machine(source):
    """pc386_machine without its display plane."""
    result, changes = source, []
    for start, end, pinned, after in MACHINE:
        if result.count(start) != 1 or result.count(end) != 1:
            raise ValueError("pc386 machine plane boundary changed")
        first, last = result.index(start), result.index(end)
        removed = result[first:last]
        if first > last or sha(removed) != pinned:
            raise ValueError("pc386 machine plane block changed")
        result = result[:first] + after + result[last:]
        changes.append({"removed_sha256": pinned, "removed_bytes": len(removed), "after": after.decode()})
    return result, _record(source, result, changes)


def retarget_system(source):
    """pc386_system with board display opcodes and the ATA-only media contract."""
    result = source
    for before, after, count in SYSTEM:
        if result.count(before) != count:
            raise ValueError("pc386 system adaptation contract changed")
        result = result.replace(before, after)
    if result.count(SYSTEM_STORAGE_START) != 1 or result.count(SYSTEM_STORAGE_END) != 1:
        raise ValueError("pc386 system storage boundary changed")
    first, last = result.index(SYSTEM_STORAGE_START), result.index(SYSTEM_STORAGE_END)
    removed = result[first:last]
    if first >= last or sha(removed) != SYSTEM_STORAGE_SHA256:
        raise ValueError("pc386 system storage block changed")
    result = result[:first] + SYSTEM_STORAGE_AFTER + result[last:]
    changes = [{"before": before.decode(), "after": after.decode(), "count": count}
               for before, after, count in SYSTEM]
    changes.append({"removed_sha256": SYSTEM_STORAGE_SHA256, "removed_bytes": len(removed),
                    "after": SYSTEM_STORAGE_AFTER.decode()})
    return result, _record(source, result, changes)


def retarget_at_core(source):
    """pc386_at_core with DMA controller 2 as its register file only."""
    result = source
    for before, after, count in AT_CORE:
        if result.count(before) != count:
            raise ValueError("pc386 AT core DMA controller 2 instance changed")
        result = result.replace(before, after)
    return result, _record(source, result, [{"before": b.decode(), "after": a.decode(), "count": n}
                                            for b, a, n in AT_CORE])


# The 486DX model's CPU is the DE25's i486DX (i486dx/derive.py), fes_i386_cpu's DE25-only
# derivation, which keeps its parameters and ports.
SYSTEM_CPU_BEFORE = b"    fes_i386_cpu #(.GOV_N(GOV_N), .GOV_D(GOV_D), .I486(I486)) cpu (\n"
SYSTEM_CPU_AFTER = b"    fes_i486dx_cpu #(.GOV_N(GOV_N), .GOV_D(GOV_D), .I486(I486)) cpu (\n"


def retarget_system_cpu(source):
    """The 486DX model's pc386_system: retarget_system's, with its CPU the DE25's i486DX."""
    if source.count(SYSTEM_CPU_BEFORE) != 1:
        raise ValueError("pc386 system CPU instance changed")
    result = source.replace(SYSTEM_CPU_BEFORE, SYSTEM_CPU_AFTER)
    return result, _record(source, result, [{"before": SYSTEM_CPU_BEFORE.decode(), "after": SYSTEM_CPU_AFTER.decode(),
                                             "count": 1}])
