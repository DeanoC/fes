"""DE25 derivations of the unchanged DE10 Commodore 64 machine sources."""
import hashlib

# The linked ROM lanes become the uploaded firmware RAM, with the same read
# ports and latencies (shared-rtl/fes_c64_firmware_rom.v).
ROM_BEFORE = (b"    c64_rom rom (\n"
              b"        .clk(clk_sys), .address(rom_address), .data(rom_q),\n"
              b"        .video_clk(clk_sys), .video_address(video_ram_addr[11:0]), .video_data(video_char_q),\n"
              b"        .drive_address(drive_rom_address), .drive_data(drive_rom_data)\n"
              b"    );\n")
ROM_AFTER = (b"    fes_c64_firmware_rom rom (\n"
             b"        .clk(clk_sys), .address(rom_address), .data(rom_q),\n"
             b"        .video_address(video_ram_addr[11:0]), .video_data(video_char_q),\n"
             b"        .drive_address(drive_rom_address), .drive_data(drive_rom_data),\n"
             b"        .write_addr(firmware_write_addr), .write_data(firmware_write_data),\n"
             b"        .write_enable(firmware_write_enable)\n"
             b"    );\n")
PORTS_BEFORE = b"    output wire [15:0] debug_pc\n);\n"
PORTS_AFTER = (b"    output wire [15:0] debug_pc,\n"
               b"    // Launch-time firmware (fes.fabric.firmware-upload), written while held.\n"
               b"    input  wire [15:0] firmware_write_addr,\n"
               b"    input  wire [15:0] firmware_write_data,\n"
               b"    input  wire [1:0]  firmware_write_enable\n);\n")

# The DE10 machine uses its CPU read bus, read_data, in the VIC's port list
# before the line that declares and assigns it. Yosys and Verilator resolve the
# forward reference; Quartus makes an implicit one-bit net at the first use and
# then drops the later declaration's assignment, leaving the 6510's read data
# undriven. Here the bus is declared ahead of the VIC and the old declaration
# becomes its assignment: the same logic, with no forward reference.
READ_BUS_ANCHOR = b"    wire [7:0] ram_q, rom_q;\n"
READ_BUS_DECLARATION = b"    wire [7:0] read_data;  // DE25: declared before the VIC uses it\n"
READ_BUS_BEFORE = b"    wire [7:0] read_data =\n"
READ_BUS_AFTER = b"    assign read_data =\n"

# Inferred memories whose Cyclone V block hint is the only family dependence.
RAM_FILES = {
    "c64_ram.v": ((b'(* ramstyle = "M10K" *)', b'(* ramstyle = "M20K" *)', 1),),
    "c64_color_ram.v": ((b'(* ramstyle = "M10K" *)', b'(* ramstyle = "M20K" *)', 1),),
}

# The DE10 disk store is one byte array with two write ports, one of them
# shared with the 1541's read; Quartus cannot map that read-during-write
# arrangement to block RAM. Here the image is two byte lanes, even and odd
# addresses, each with one write port and one read port. A mailbox byte pair
# is two consecutive addresses, so its bytes always land in different lanes.
# The 1541's read keeps its one clock: both lanes read the address's row and
# the registered low address bit selects the lane. The DE10 port A returned
# the byte being written while a pair was written; here a read always returns
# read_addr, and the 1541 reads no image while the unit is loading.
DISK_BEGIN = b'    (* ram_style = "m10k_tdp" *) reg [7:0] image [0:BYTES-1];\n'
DISK_END = b"endmodule\n"
DISK_AFTER = b"""    // Two byte lanes (DE25 derivation); see derive.py.
    localparam integer ROWS = BYTES / 2;
    (* ramstyle = "M20K" *) reg [7:0] even [0:ROWS-1];
    (* ramstyle = "M20K" *) reg [7:0] odd [0:ROWS-1];
    wire [17:0] high_addr = write_addr + 18'd1;
    // The byte bound for each lane this clock, low byte first.
    wire even_low = !write_addr[0];
    wire even_write = even_low ? write_enable[0] : write_enable[1];
    wire odd_write = even_low ? write_enable[1] : write_enable[0];
    wire [16:0] even_row = even_low ? write_addr[17:1] : high_addr[17:1];
    wire [16:0] odd_row = even_low ? high_addr[17:1] : write_addr[17:1];
    wire [7:0] even_data = even_low ? write_data[7:0] : write_data[15:8];
    wire [7:0] odd_data = even_low ? write_data[15:8] : write_data[7:0];
    reg [7:0] even_q, odd_q;
    reg read_odd;
    always @(posedge clk) begin
        if (even_write) even[even_row] <= even_data;
        even_q <= even[read_addr[17:1]];
    end
    always @(posedge clk) begin
        if (odd_write) odd[odd_row] <= odd_data;
        odd_q <= odd[read_addr[17:1]];
    end
    always @(posedge clk) read_odd <= read_addr[0];
    always @* read_data = read_odd ? odd_q : even_q;
endmodule
"""

# The DE10 frame store is 153 placed 2048x5 M10K primitives; its simulation
# branch is the same three pages as plain memories. Here each page is one
# inferred memory with its own write statement, for Quartus to shape: the
# same words at the same addresses, written in the system clock and read with
# one pixel edge, as the DE10 primitives are.
STORE_BEGIN = b"`ifdef FES_SIMULATION\n"
STORE_END = b"`endif\n"
STORE_AFTER = b"""    // One inferred memory per page (DE25 derivation); see derive.py.
    (* ramstyle = "M20K" *) reg [3:0] page0 [0:104447];
    (* ramstyle = "M20K" *) reg [3:0] page1 [0:104447];
    (* ramstyle = "M20K" *) reg [3:0] page2 [0:104447];
    reg [3:0] q0, q1, q2;
    wire page_write = !reset && write_enable;
    always @(posedge clk_sys)
        if (page_write && write_page == 2'd0) page0[write_address] <= write_data;
    always @(posedge clk_sys)
        if (page_write && write_page == 2'd1) page1[write_address] <= write_data;
    always @(posedge clk_sys)
        if (page_write && write_page == 2'd2) page2[write_address] <= write_data;
    always @(posedge pixel_clk) begin
        q0 <= page0[read_address]; q1 <= page1[read_address]; q2 <= page2[read_address];
    end
"""
# The frame store's crossings take the shape the Spectrum and Atari ST pass
# signoff with. On the DE10:
# - the reader samples the machine reset, a combination of machine registers,
#   directly in the pixel clock;
# - each two-register toggle synchronizer also takes its side's synchronous
#   reset;
# - the two-bit published page goes straight from register to register into
#   read_page and on into read_page_q, so Design Assistant reads it as two
#   synchronizers.
# Here:
# - the reset is registered once in the system clock and enters the pixel
#   clock through two registers, so the reader resets one system and two
#   pixel clocks later;
# - the synchronizers are plain registers that power up zero; each toggle's
#   launch register still resets, and a machine reset lasts more than either
#   synchronizer, so both sides agree again once released;
# - the page crosses as a held code, the page with its low bit flipped, that
#   powers up zero at both ends and is loaded on the synchronized strobe. The
#   reader's page is that code with its loaded flag, which is page 2 until the
#   first load, as the DE10's reset leaves it; no frame shows before then.
CROSSING = (
    (b"    reg [1:0] published_page, read_page_q;\n",
     b"    reg [1:0] published_page, read_page_q;\n"
     b"    (* preserve, dont_merge *) reg [1:0] published_code = 2'd0;\n"
     b"    (* preserve, dont_merge *) reg [1:0] read_code = 2'd0;\n"
     b"    reg read_loaded = 1'b0;\n"
     b"    always @* read_page = read_code ^ {~read_loaded, read_loaded};\n", 1),
    (b'    (* async_reg="true" *) reg ack_meta, ack_sync;\n'
     b'    (* async_reg="true" *) reg request_meta, request_sync;\n',
     b'    (* async_reg="true" *) reg ack_meta = 1\'b0, ack_sync = 1\'b0;\n'
     b'    (* async_reg="true" *) reg request_meta = 1\'b0, request_sync = 1\'b0;\n', 1),
    (b"            write_page <= 0; published_page <= 2; publish_toggle <= 0;\n"
     b"            ack_meta <= 0; ack_sync <= 0;\n",
     b"            write_page <= 0; published_page <= 2; publish_toggle <= 0;\n", 1),
    (b"                published_page <= write_page; publish_toggle <= ~publish_toggle;\n",
     b"                published_page <= write_page; publish_toggle <= ~publish_toggle;\n"
     b"                published_code <= write_page ^ 2'd1;\n", 1),
    (b"    always @(posedge pixel_clk) begin\n        request_meta <= publish_toggle;",
     b"    (* preserve, dont_merge *) reg reset_source = 1'b1;\n"
     b"    always @(posedge clk_sys) reset_source <= reset;\n"
     b'    (* async_reg="true" *) reg reset_meta = 1\'b1;\n'
     b'    (* async_reg="true" *) reg reset_pixel = 1\'b1;\n'
     b"    always @(posedge pixel_clk) begin\n"
     b"        reset_meta <= reset_source; reset_pixel <= reset_meta;\n"
     b"    end\n"
     b"    always @(posedge pixel_clk) begin\n        request_meta <= publish_toggle;", 1),
    (b"        if (reset) begin\n"
     b"            read_page <= 2; acknowledge <= 0; have_frame <= 0;\n"
     b"            request_meta <= 0; request_sync <= 0; have_frame_q <= 0;\n",
     b"        if (reset_pixel) begin\n"
     b"            acknowledge <= 0; have_frame <= 0; have_frame_q <= 0;\n", 1),
    (b"            read_page <= published_page; acknowledge <= request_sync; have_frame <= 1;\n",
     b"            read_code <= published_code; read_loaded <= 1'b1;\n"
     b"            acknowledge <= request_sync; have_frame <= 1;\n", 1),
)

TRANSPORT_MARKER = b"// Atomic register transport shared by the bring-up and standard-core wrappers.\n"


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
    """Swap the linked ROM lanes for the uploaded firmware RAM; declare the CPU read bus first."""
    if b"MISTRAL" in source or b"c64_rom " in source.replace(ROM_BEFORE, b""):
        raise ValueError("C64 machine ROM adaptation contract changed")
    first_use = source.find(b"read_data")
    if first_use < 0 or first_use >= source.find(READ_BUS_BEFORE) or source.find(READ_BUS_ANCHOR) > first_use:
        raise ValueError("C64 machine read bus adaptation contract changed")
    return _derive(source, ((ROM_BEFORE, ROM_AFTER, 1), (PORTS_BEFORE, PORTS_AFTER, 1),
                            (READ_BUS_ANCHOR, READ_BUS_ANCHOR + READ_BUS_DECLARATION, 1),
                            (READ_BUS_BEFORE, READ_BUS_AFTER, 1)), "C64 machine ROM")


def retarget_ram(name, source):
    """Block RAM hints for this family."""
    return _derive(source, RAM_FILES[name], "C64 " + name)


def retarget_disk_store(source):
    """Two single-write byte lanes in place of the shared-port byte array."""
    if source.count(DISK_BEGIN) != 1 or source.count(DISK_END) != 1:
        raise ValueError("C64 disk store adaptation contract changed")
    start = source.index(DISK_BEGIN)
    stop = source.index(DISK_END) + len(DISK_END)
    removed = source[start:stop]
    if (b"localparam integer BYTES = 174848;" not in source[:start] or
            removed.count(b"image[address_a] <= write_data[7:0];") != 1 or
            removed.count(b"image[address_b] <= write_data[15:8];") != 1 or
            removed.count(b"read_data <= image[address_a];") != 1):
        raise ValueError("C64 disk store adaptation contract changed")
    result = source[:start] + DISK_AFTER + source[stop:]
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "removed_sha256": sha(removed), "storage": DISK_AFTER.decode()}


def retarget_frame_store(source):
    """Inferred pages in place of the placed M10K primitives; signoff-shaped crossings."""
    if source.count(STORE_BEGIN) != 1 or source.count(STORE_END) != 1:
        raise ValueError("C64 frame store adaptation contract changed")
    start = source.index(STORE_BEGIN)
    stop = source.index(STORE_END) + len(STORE_END)
    removed = source[start:stop]
    if (stop < start or removed.count(b"MISTRAL_M10K") != 1 or b"MISTRAL" in source[:start] + source[stop:] or
            b"reg [3:0] page0 [0:104447];" not in removed):
        raise ValueError("C64 frame store adaptation contract changed")
    stored = source[:start] + STORE_AFTER + source[stop:]
    result, record = _derive(stored, CROSSING, "C64 frame store crossing")
    record["source_sha256"] = sha(source)
    record["removed_sha256"] = sha(removed)
    record["storage"] = STORE_AFTER.decode()
    return result, record


def transport(source):
    """Only the shared atomic transport from the ZX81 computer file, unchanged."""
    if source.count(TRANSPORT_MARKER) != 1:
        raise ValueError("DE25 computer transport boundary changed")
    result = (b'`include "fes_computer.vh"\n`include "generated/fes_de25_computer.vh"\n' +
              source[source.index(TRANSPORT_MARKER):])
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "selection": "fes_de25_computer_transport and its two includes"}
