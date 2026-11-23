"""DE25 derivations of the unchanged DE10 pc386 machine sources.

The DE10 machine carries its own session-display plane (fes_display_plane
behind pc386_plane_bridge), which reads the launcher slots from window offset
0 on DDR port 2. A DE25 machine shows its picture through the board's
fes_de25_session_display instead, which owns the scanout's own grant. So the
plane comes out of pc386_machine: video is the presenter's 720p word, the
plane's port-2 requester is idle, and pc386_system carries the mailbox's
display opcodes out to the board in place of into the machine.

pc386_kbc's two PS/2 FIFOs are pc386_ps2_fifo_mlab.sv: one write port, so
their arrays are MLAB RAM. It is cycle-exact with pc386_ps2_fifo at its
ports, and the DE10 keeps pc386_ps2_fifo.

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

KBC_FIFO_START = b"// 16-byte FIFO for the PS/2 devices' bytes to the controller.\nmodule pc386_ps2_fifo ("
KBC_FIFO_SHA256 = "4007de5c756f5a3586010893181834affe5ddcf89ba3302f485144665c7e9125"
KBC_FIFO_BEFORE = b"    pc386_ps2_fifo fifo ("
KBC_FIFO_AFTER = b"    pc386_ps2_fifo_mlab fifo ("
KBC_FIFOS = 2


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


def retarget_kbc(source):
    """pc386_kbc with its PS/2 FIFOs as pc386_ps2_fifo_mlab.

    The DE10 module the replacement matches is pinned by its SHA-256, so a
    change to it fails here rather than leaving the two forms apart.
    """
    if source.count(KBC_FIFO_START) != 1:
        raise ValueError("pc386 PS/2 FIFO module changed")
    first = source.index(KBC_FIFO_START)
    module = source[first:source.index(b"endmodule\n", first) + len(b"endmodule\n")]
    if sha(module) != KBC_FIFO_SHA256:
        raise ValueError("pc386 PS/2 FIFO module changed")
    if source.count(KBC_FIFO_BEFORE) != KBC_FIFOS:
        raise ValueError("pc386 PS/2 FIFO instances changed")
    result = source.replace(KBC_FIFO_BEFORE, KBC_FIFO_AFTER)
    return result, _record(source, result, [{"module_sha256": KBC_FIFO_SHA256, "before": KBC_FIFO_BEFORE.decode(),
                                             "after": KBC_FIFO_AFTER.decode(), "count": KBC_FIFOS}])
