"""DE25 derivations of the unchanged DE10 ZX Spectrum machine sources."""
import hashlib

ROM_BEFORE = (b"    spectrum_rom #(.ROM_FILE(ROM_FILE)) rom (\n"
              b"        .clk(clk_sys), .address({cpu_map[1:0], cpu_a[13:0]}), .data(rom_data)\n"
              b"    );\n")
ROM_AFTER = (b"    fes_spectrum_firmware_rom rom (\n"
             b"        .clk(clk_sys), .address({cpu_map[1:0], cpu_a[13:0]}), .data(rom_data),\n"
             b"        .write_addr(firmware_write_addr), .write_data(firmware_write_data),\n"
             b"        .write_enable(firmware_write_enable)\n"
             b"    );\n")
PORTS_BEFORE = b"    output wire [15:0] cpu_pc\n);\n"
PORTS_AFTER = (b"    output wire [15:0] cpu_pc,\n"
               b"    // Launch-time firmware (fes.fabric.firmware-upload), written while held.\n"
               b"    input  wire [15:0] firmware_write_addr,\n"
               b"    input  wire [15:0] firmware_write_data,\n"
               b"    input  wire [1:0]  firmware_write_enable\n);\n")
PARAMETER_BEFORE = (b"module spectrum_machine #(\n"
                    b'    parameter ROM_FILE = "build/diagnostics/fes-spectrum/firmware.hex"\n'
                    b") (\n")
PARAMETER_AFTER = b"module spectrum_machine (\n"
# Inferred memories whose Cyclone V block hint is the only family dependence.
RAM_FILES = {"spectrum_ram.v": 1, "spectrum_video.v": 1, "spectrum_tape.v": 1}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def _derive(source, replacements, label):
    result = source
    for before, after, count in replacements:
        if result.count(before) != count:
            raise ValueError(label + " adaptation contract changed")
        result = result.replace(before, after)
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "changes": [{"before": a.decode(), "after": b.decode(), "count": n}
                                for a, b, n in replacements]}


def retarget_machine(source):
    """Swap the linked ROM lanes for the uploaded firmware RAM and its write port."""
    return _derive(source, ((PARAMETER_BEFORE, PARAMETER_AFTER, 1), (ROM_BEFORE, ROM_AFTER, 1),
                            (PORTS_BEFORE, PORTS_AFTER, 1)), "Spectrum machine ROM")


# The DE10 frame store passes the pixel side a {toggle, bank} word and takes
# the shown bank back, each bit through its own two-stage synchronizer: a
# receiving edge between two bits' arrivals can see a mixed word. Here only a
# toggle crosses a synchronizer in each direction, and the bank is held behind
# it. The pixel side takes the ready bank at its own frame end, as the DE10
# source does: a ready bank stands for a whole machine frame, about 20 ms, and
# a pixel frame end comes within 16.7 ms of the toggle's arrival. The frame end
# is a registered flag, so the load's enable is that flag and the toggle
# comparison. ready_seen flips with every such load, so it is also the toggle
# that carries the shown bank back, which the machine side copies once it
# arrives.
# The banks cross as codes that power up zero. The DE10 registers power up as
# banks 1 and 2, and a register that powers up high is built inverted, which
# puts an inverter on a register-to-register crossing. The ready code is the
# bank with its low bit flipped; each side reads bank 2 until its first load.
# The code registers are kept apart from the bank registers they resemble, so
# each crossing has its own launch and landing.
VIDEO_CROSSING = (
    (b"    reg [1:0] shown_bank = 2'd2;\n"
     b'    (* async_reg = "true" *) reg [1:0] shown_meta = 2\'d2;\n'
     b'    (* async_reg = "true" *) reg [1:0] shown_sync = 2\'d2;\n',
     b"    (* preserve, dont_merge *) reg [1:0] ready_code = 2'd0;\n"
     b"    (* preserve, dont_merge *) reg [1:0] shown_code = 2'd0;\n"
     b"    reg shown_loaded = 1'b0;\n"
     b"    wire [1:0] shown_bank = shown_code ^ {~shown_loaded, shown_loaded};\n"
     b'    (* async_reg = "true" *) reg shown_meta = 1\'b0;\n'
     b'    (* async_reg = "true" *) reg shown_arrived = 1\'b0;\n'
     b"    reg shown_taken = 1'b0;\n"
     b"    (* preserve, dont_merge *) reg [1:0] shown_code_sys = 2'd0;\n"
     b"    reg shown_loaded_sys = 1'b0;\n"
     b"    wire [1:0] shown_sync = shown_code_sys ^ {~shown_loaded_sys, shown_loaded_sys};\n", 1),
    (b"        shown_meta <= shown_bank;\n"
     b"        shown_sync <= shown_meta;\n",
     b"        shown_meta <= ready_seen;\n"
     b"        shown_arrived <= shown_meta;\n"
     b"        shown_taken <= shown_arrived;\n"
     b"        if (shown_arrived != shown_taken) begin\n"
     b"            shown_code_sys <= shown_code;\n"
     b"            shown_loaded_sys <= 1'b1;\n"
     b"        end\n", 1),
    (b"            ready_bank <= target_bank;\n",
     b"            ready_bank <= target_bank;\n"
     b"            ready_code <= target_bank ^ 2'd1;\n", 1),
    (b'    (* async_reg = "true" *) reg [2:0] ready_meta = 3\'b001;\n'
     b'    (* async_reg = "true" *) reg [2:0] ready_sync = 3\'b001;\n',
     b'    (* async_reg = "true" *) reg ready_meta = 1\'b0;\n'
     b'    (* async_reg = "true" *) reg ready_sync = 1\'b0;\n'
     b"    reg frame_last = 1'b0;\n", 1),
    (b"        ready_meta <= {ready_toggle, ready_bank};\n"
     b"        ready_sync <= ready_meta;\n",
     b"        ready_meta <= ready_toggle;\n"
     b"        ready_sync <= ready_meta;\n"
     b"        frame_last <= h == H_TOTAL - 2'd2 && v == V_TOTAL - 1'b1;\n"
     b"        if (ready_sync != ready_seen && frame_last) begin\n"
     b"            ready_seen <= ready_sync;\n"
     b"            shown_code <= ready_code;\n"
     b"            shown_loaded <= 1'b1;\n"
     b"        end\n", 1),
    (b"                if (ready_sync[2] != ready_seen) begin\n"
     b"                    ready_seen <= ready_sync[2];\n"
     b"                    shown_bank <= ready_sync[1:0];\n"
     b"                end\n", b"", 1),
)

def retarget_ram(name, source):
    """Block RAM hints for this family and, for the video, the frame-bank crossing."""
    return _derive(source, ((b'ramstyle = "M10K"', b'ramstyle = "M20K"', RAM_FILES[name]),
                            *(VIDEO_CROSSING if name == "spectrum_video.v" else ())),
                   "Spectrum " + name)


TRANSPORT_MARKER = b"// Atomic register transport shared by the bring-up and standard-core wrappers.\n"


def transport(source):
    """Opt-in fetch observation over the existing atomic transport."""
    if source.count(TRANSPORT_MARKER) != 1:
        raise ValueError("DE25 computer transport boundary changed")
    body = source[source.index(TRANSPORT_MARKER):]
    # The native and Linux transports intentionally use different semantics at
    # byte offset 0x18. Keep selection compile-time and package-bound.
    result, record = _derive(body, (
        (b"// Atomic register transport",
         b'`include "fes_computer.vh"\n'
         b'`ifdef FES_DE25_NATIVE_SPECTRUM48\n'
         b'`include "generated/fes_native_de25_computer.vh"\n'
         b'`else\n`include "generated/fes_de25_computer.vh"\n`endif\n'
         b"// Atomic register transport", 1),
        (b"output wire transport_ready, faulted\n",
         b"output wire transport_ready, faulted\n"
         b"`ifdef FES_DE25_NATIVE_SPECTRUM48\n"
         b"    , input wire machine_clk, input wire [31:0] fetch_count,\n"
         b"    input wire [15:0] fetch_address\n`endif\n", 1),
        (b"    assign waitrequest = !reset_n || !first_seen;",
         FETCH_SNAPSHOT, 1),
        (b"    wire [31:0] read_fault = read && !read_address_valid ?\n"
         b"        `FES_DE25_COMPUTER_FAULT_ADDRESS : 32'd0;",
         b"    wire [31:0] read_fault = (read && !read_address_valid ?\n"
         b"        `FES_DE25_COMPUTER_FAULT_ADDRESS : 32'd0) |\n"
         b"        (fetch_timeout ? `FES_DE25_COMPUTER_FAULT_BUSY : 32'd0);", 1),
        (b"    wire access_error = (read && !read_address_valid) ||",
         b"    wire access_error = read_fault != 32'd0 ||", 1),
        (b"            `FES_DE25_COMPUTER_VIDEO_STATUS_OFFSET: read_value = video_status;",
         b"`ifdef FES_DE25_NATIVE_SPECTRUM48\n"
         b"            `FES_DE25_COMPUTER_FETCH_COUNT_OFFSET: read_value = fetch_sample[47:16];\n"
         b"            `FES_DE25_COMPUTER_FETCH_ADDRESS_OFFSET: read_value = {16'd0, sampled_address};\n"
         b"`else\n"
         b"            `FES_DE25_COMPUTER_VIDEO_STATUS_OFFSET: read_value = video_status;\n"
         b"`endif", 1),
        (b"    wire [31:0] video_status = transport_ready && video_ready_sync[1] ?\n"
         b"        `FES_DE25_COMPUTER_VIDEO_READY : 32'd0;",
         b"`ifndef FES_DE25_NATIVE_SPECTRUM48\n"
         b"    wire [31:0] video_status = transport_ready && video_ready_sync[1] ?\n"
         b"        `FES_DE25_COMPUTER_VIDEO_READY : 32'd0;\n`endif", 1),
    ), "Spectrum native fetch")
    record["source_sha256"] = sha(source)
    return result, record


FETCH_SNAPSHOT = b"""
`ifdef FES_DE25_NATIVE_SPECTRUM48
    wire count_read = read && byte_address == `FES_DE25_COMPUTER_FETCH_COUNT_OFFSET;
    (* preserve, dont_merge *) reg fetch_request = 1'b0, fetch_reply = 1'b0;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] fetch_request_sync = 2'b00, fetch_reply_sync = 2'b00;
    (* preserve, dont_merge *) reg [47:0] fetch_payload = 48'd0, fetch_sample = 48'd0;
    reg [15:0] sampled_address = 16'd0;
    reg fetch_pending = 1'b0, fetch_ready = 1'b0;
    reg [5:0] fetch_wait = 6'd0;
    wire fetch_timeout = count_read && fetch_pending && !fetch_ready && (&fetch_wait);

    // A count read waits for one fresh machine-clock snapshot. The last
    // address is latched by that same successful Avalon transfer.
    always @(posedge machine_clk) begin
        fetch_request_sync <= {fetch_request_sync[0], fetch_request};
        if (fetch_request_sync[1] != fetch_reply) begin
            fetch_payload <= {fetch_count, fetch_address};
            fetch_reply <= fetch_request_sync[1];
        end
    end
    always @(posedge clk) begin
        fetch_reply_sync <= {fetch_reply_sync[0], fetch_reply};
        if (reset_n && first_seen && count_read && !fetch_pending) begin
            fetch_request <= ~fetch_request;
            fetch_pending <= 1'b1;
            fetch_wait <= 6'd0;
        end else if (fetch_pending && !fetch_ready && fetch_reply_sync[1] == fetch_request) begin
            fetch_sample <= fetch_payload;
            fetch_ready <= 1'b1;
        end else if (fetch_pending && !fetch_ready && !(&fetch_wait)) begin
            fetch_wait <= fetch_wait + 6'd1;
        end
        if (count_read && !waitrequest && fetch_ready) begin
            sampled_address <= fetch_sample[15:0];
            fetch_pending <= 1'b0;
            fetch_ready <= 1'b0;
        end
    end
    // A stopped machine clock must not hang an HPS MMIO read. Only
    // reconfiguration can recover the transport after this sticky fault.
    assign waitrequest = !reset_n || !first_seen ||
                         (count_read && !fault_active && !fetch_ready && !fetch_timeout);
`else
    wire fetch_timeout = 1'b0;
    assign waitrequest = !reset_n || !first_seen;
`endif
"""
