"""DE25 derivations of the unchanged DE10 pc386 machine sources.

The DE10 machine carries its own session-display plane (fes_display_plane
behind pc386_plane_bridge), which reads the launcher slots from window offset
0 on DDR port 2. A DE25 machine shows its picture through the board's
fes_de25_session_display instead, which owns the scanout's own grant. So the
plane comes out of pc386_machine: video is the presenter's 720p word, the
plane's port-2 requester is idle, and pc386_system carries the mailbox's
display opcodes out to the board in place of into the machine.

Controller 2 of the AT core's DMA is its register file only
(pc386_dma2_registers.sv): it reads back as fes_dma8237 does, but runs no
cycle, so a software request on channels 4-7 does not count down or set TC.
Nothing in the machine raises a controller-2 DREQ. The DE10 keeps the full
controller.

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
SYSTEM = ((SYSTEM_WIRES_BEFORE, b"", 1), (SYSTEM_PORTS_BEFORE, SYSTEM_PORTS_AFTER, 1),
          (SYSTEM_MACHINE_BEFORE, SYSTEM_MACHINE_AFTER, 1), (SYSTEM_SNAPSHOT_BEFORE, SYSTEM_SNAPSHOT_AFTER, 1))

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
    """pc386_system with the mailbox's display opcodes carried out to the board."""
    result = source
    for before, after, count in SYSTEM:
        if result.count(before) != count:
            raise ValueError("pc386 system display adaptation contract changed")
        result = result.replace(before, after)
    return result, _record(source, result, [{"before": b.decode(), "after": a.decode(), "count": n}
                                            for b, a, n in SYSTEM])


def retarget_at_core(source):
    """pc386_at_core with DMA controller 2 as its register file only."""
    result = source
    for before, after, count in AT_CORE:
        if result.count(before) != count:
            raise ValueError("pc386 AT core DMA controller 2 instance changed")
        result = result.replace(before, after)
    return result, _record(source, result, [{"before": b.decode(), "after": a.decode(), "count": n}
                                            for b, a, n in AT_CORE])


def retarget_window_guard(source):
    """Discard an old bus epoch when the DE25 response path is reset."""
    pinned = "583572b89a4b4a645981e57e7af3c7ae7e29fe7386814452a1dab150523d8ebf"
    if sha(source) != pinned:
        raise ValueError("review the DE25 reset against the changed DDR guard")
    changes = (
        (b"    input  wire                hold,", b"    input  wire                reset, hold,"),
        (b"assign waitrequest = skid", b"assign waitrequest = reset | skid"),
        (b"wire filler = finishing", b"wire filler = ~reset & finishing"),
        (b"        readdatavalid <= returned & ~hold & ~draining;\n",
         b"        readdatavalid <= returned & ~hold & ~draining;\n"
         b"        // The bridge also resets, so this epoch will never receive its responses.\n"
         b"        if (reset) begin\n"
         b"            head <= 1'b0; tail <= 1'b0; present <= 1'b0; skid <= 1'b0;\n"
         b"            beats_owed <= 8'd0; owed_zero <= 1'b1; owed_one <= 1'b0;\n"
         b"            reads_owed <= 12'd0; finishing <= 1'b0; draining <= 1'b0;\n"
         b"            returned <= 1'b0; readdatavalid <= 1'b0;\n"
         b"        end\n"),
    )
    result = source
    for before, after in changes:
        if result.count(before) != 1:
            raise ValueError("DE25 DDR guard reset anchor changed")
        result = result.replace(before, after)
    return result, _record(source, result, [{"before": b.decode(), "after": a.decode()} for b, a in changes])
