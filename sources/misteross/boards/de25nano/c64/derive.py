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

# The DE10 machine's system reset is one combinational term that reaches some
# six thousand registers; Quartus signoff refuses the two registers behind it
# for endpoint tension (TMC-20602), as it refused the Atari ST's single machine
# reset. Here system_reset is registered, and each subsystem instance takes its
# own preserved register of the same term. Every copy asserts and releases on
# the same machine clock, one clock after the DE10's term; the CPU still starts
# on the same Phi2 cycle, since the release follows reset_hold, which counts
# Phi2 ticks.
RESET_GROUPS = ("cpu", "cia1", "cia2", "drive", "gcr", "vic", "sid")
RESET_BEFORE = b"    wire system_reset = reset || reset_hold != 4'd0;\n"
RESET_AFTER = (b"    // DE25: the system reset is registered, one copy per subsystem; see derive.py.\n"
               b"    wire system_reset_next = reset || reset_hold != 4'd0;\n"
               b"    (* preserve, dont_merge *) reg system_reset = 1'b1;\n" +
               b"".join(b"    (* preserve, dont_merge *) reg reset_" + g.encode() + b" = 1'b1;\n" for g in RESET_GROUPS) +
               b"    always @(posedge clk_sys) begin\n"
               b"        system_reset <= system_reset_next;\n" +
               b"".join(b"        reset_" + g.encode() + b" <= system_reset_next;\n" for g in RESET_GROUPS) +
               b"    end\n")
RESET_USES = (
    (b".clk(clk_sys), .reset(system_reset), .AB(cpu_ab)", b".clk(clk_sys), .reset(reset_cpu), .AB(cpu_ab)"),
    (b".tod_tick(tod_tick), .reset(system_reset), .cs(sel_cia1)", b".tod_tick(tod_tick), .reset(reset_cia1), .cs(sel_cia1)"),
    (b".tod_tick(tod_tick), .reset(system_reset), .cs(sel_cia2)", b".tod_tick(tod_tick), .reset(reset_cia2), .cs(sel_cia2)"),
    (b"    c64_drive1541 drive (\n        .clk(clk_sys), .reset(system_reset),",
     b"    c64_drive1541 drive (\n        .clk(clk_sys), .reset(reset_drive),"),
    (b".reset(system_reset), .ref_ce(drive_ref_ce), .flux(drive_flux)",
     b".reset(reset_gcr), .ref_ce(drive_ref_ce), .flux(drive_flux)"),
    (b".pixel_clk(video_clk), .reset(system_reset),", b".pixel_clk(video_clk), .reset(reset_vic),"),
    (b".phi(cpu_ce), .reset(system_reset), .cs(sel_sid)", b".phi(cpu_ce), .reset(reset_sid), .cs(sel_sid)"),
)

# Inferred memories whose Cyclone V block hint is the only family dependence.
RAM_FILES = {
    "c64_ram.v": ((b'(* ramstyle = "M10K" *)', b'(* ramstyle = "M20K" *)', 1),),
    "c64_color_ram.v": ((b'(* ramstyle = "M10K" *)', b'(* ramstyle = "M20K" *)', 1),),
}

# Replace the fixed one-clock D64 image reads with held requests. The GCR
# track cache and rotation remain local to the 1541.
MEDIA_PORTS = b"""    input  wire [17:0] media_write_addr,
    input  wire [15:0] media_write_data,
    input  wire [1:0]  media_write_enable,
"""
FETCH_PORTS = b"""    output wire [17:0] disk_addr,
    output wire        disk_fetch,
    input  wire        disk_fetch_ready,
    input  wire [7:0]  disk_byte,
"""
DISK_INSTANCE = b"""    c64_disk_store disk (
        .clk(clk_sys), .write_addr(media_write_addr), .write_data(media_write_data),
        .write_enable(media_write_enable), .read_addr(disk_addr), .read_data(disk_byte)
    );
"""

# The VIC writes an even-sized raster in address order. Pack adjacent
# four-bit pixels into a byte so a page uses 26 M20Ks instead of 51.
STORE_BEGIN = b"`ifdef FES_SIMULATION\n"
STORE_END = b"`endif\n"
STORE_AFTER = b"""    wire page_write = !reset && write_enable;
    reg [3:0] even_pixel;
    reg read_odd;
    always @(posedge clk_sys)
        if (page_write && !write_address[0]) even_pixel <= write_data;
    always @(posedge pixel_clk) read_odd <= read_address[0];
    (* ramstyle = "M20K" *) reg [7:0] page0 [0:52223];
    reg [7:0] pair0;
    wire [3:0] q0 = read_odd ? pair0[7:4] : pair0[3:0];
    always @(posedge clk_sys)
        if (page_write && write_page == 2'd0 && write_address[0])
            page0[write_address[16:1]] <= {write_data, even_pixel};
    always @(posedge pixel_clk) pair0 <= page0[read_address[16:1]];
    (* ramstyle = "M20K" *) reg [7:0] page1 [0:52223];
    reg [7:0] pair1;
    wire [3:0] q1 = read_odd ? pair1[7:4] : pair1[3:0];
    always @(posedge clk_sys)
        if (page_write && write_page == 2'd1 && write_address[0])
            page1[write_address[16:1]] <= {write_data, even_pixel};
    always @(posedge pixel_clk) pair1 <= page1[read_address[16:1]];
    (* ramstyle = "M20K" *) reg [7:0] page2 [0:52223];
    reg [7:0] pair2;
    wire [3:0] q2 = read_odd ? pair2[7:4] : pair2[3:0];
    always @(posedge clk_sys)
        if (page_write && write_page == 2'd2 && write_address[0])
            page2[write_address[16:1]] <= {write_data, even_pixel};
    always @(posedge pixel_clk) pair2 <= page2[read_address[16:1]];
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


def retarget_machine(source, *, native=False):
    """Swap the linked ROM lanes for the uploaded firmware RAM; declare the CPU read bus first."""
    if b"MISTRAL" in source or b"c64_rom " in source.replace(ROM_BEFORE, b""):
        raise ValueError("C64 machine ROM adaptation contract changed")
    first_use = source.find(b"read_data")
    if first_use < 0 or first_use >= source.find(READ_BUS_BEFORE) or source.find(READ_BUS_ANCHOR) > first_use:
        raise ValueError("C64 machine read bus adaptation contract changed")
    fetch = ()
    if native:
        declaration = b"    output wire [15:0] debug_pc,\n"
        connection = b".RDY(cpu_run_ce), .SO(1'b1)"
        fetch = ((declaration, declaration + b"    output wire decoded,\n    output wire [15:0] decoded_address,\n", 1),
                 (connection, connection + b",\n        .decoded(decoded), .decoded_address(decoded_address)", 1))
    return _derive(source, ((ROM_BEFORE, ROM_AFTER, 1), (PORTS_BEFORE, PORTS_AFTER, 1), *fetch,
                            (READ_BUS_ANCHOR, READ_BUS_ANCHOR + READ_BUS_DECLARATION, 1),
                            (READ_BUS_BEFORE, READ_BUS_AFTER, 1), (RESET_BEFORE, RESET_AFTER, 1),
                            (MEDIA_PORTS, FETCH_PORTS, 1), (DISK_INSTANCE, b"", 1),
                            (b"    wire [17:0] disk_addr;\n    wire [7:0] disk_byte;\n", b"", 1),
                            (b".disk_addr(disk_addr), .disk_data(disk_byte),",
                             b".disk_addr(disk_addr), .disk_data(disk_byte),\n"
                             b"        .disk_fetch(disk_fetch), .disk_fetch_ready(disk_fetch_ready),", 1),
                            *((before, after, 1) for before, after in RESET_USES)), "C64 machine ROM")


def retarget_ram(name, source):
    """Block RAM hints for this family."""
    return _derive(source, RAM_FILES[name], "C64 " + name)


def retarget_medium(source):
    """Wait for a valid SDRAM byte before consuming it in the GCR builder."""
    ports = b" input wire [7:0] disk_data,"
    request = b" reg [3:0] state;"
    return _derive(source, (
        (ports, ports + b"\n output disk_fetch,\n input disk_fetch_ready,", 1),
        (request, request + b"\n assign disk_fetch = !reset && disk_ready &&\n"
         b"     (state == ID_GET0 || state == ID_GET1 || state == READ_GET);", 1),
        (b"ID_GET0:begin", b"ID_GET0:if(disk_fetch_ready)begin", 1),
        (b"ID_GET1:begin", b"ID_GET1:if(disk_fetch_ready)begin", 1),
        (b"READ_GET:begin", b"READ_GET:if(disk_fetch_ready)begin", 1),
    ), "C64 D64 fetch")


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

def retarget_cpu(source):
    """Expose accepted opcode-decode activity in the native project only."""
    ports = b"RDY, SO );"
    declarations = b"wire nmi_take = branch_no_cross ? nmi_older : nmi_sampled;\n"
    return _derive(source, (
        (ports, b"RDY, SO, decoded, decoded_address );", 1),
        (declarations, declarations + b"""
output decoded;
output [15:0] decoded_address;
// The synchronous memory pipeline decodes the previous bus cycle's opcode.
// A hardware interrupt substitutes its own opcode and is not a fetch sample.
assign decoded = !reset && RDY && state == DECODE && !irq_take && !nmi_take && !IRHOLD_valid;
assign decoded_address = {ABH, ABL};
""", 1),
    ), "C64 native opcode decode")
