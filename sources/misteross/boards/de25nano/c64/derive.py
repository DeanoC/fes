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

# Inferred memories whose Cyclone V block hint is the only family dependence.
# The disk store's hint is Yosys's; Quartus reads ramstyle.
RAM_FILES = {
    "c64_ram.v": ((b'(* ramstyle = "M10K" *)', b'(* ramstyle = "M20K" *)', 1),),
    "c64_color_ram.v": ((b'(* ramstyle = "M10K" *)', b'(* ramstyle = "M20K" *)', 1),),
    "c64_disk_store.v": ((b'(* ram_style = "m10k_tdp" *)', b'(* ramstyle = "M20K" *)', 1),),
}

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
# The DE10 reader samples the system-domain machine reset directly in the
# pixel clock, and that level is a combination of machine registers. Here it
# is registered once in the system clock and enters the pixel clock through
# two registers; the reader's reset is otherwise the same and is seen one
# system and two pixel clocks later. Both sides still clear their toggles, so
# they agree again once both are released.
READER_BEFORE = b"    always @(posedge pixel_clk) begin\n        request_meta <= publish_toggle;"
READER_AFTER = (b"    (* preserve, dont_merge *) reg reset_source = 1'b1;\n"
                b"    always @(posedge clk_sys) reset_source <= reset;\n"
                b'    (* async_reg="true" *) reg reset_meta = 1\'b1;\n'
                b'    (* async_reg="true" *) reg reset_pixel = 1\'b1;\n'
                b"    always @(posedge pixel_clk) begin\n"
                b"        reset_meta <= reset_source; reset_pixel <= reset_meta;\n"
                b"    end\n"
                b"    always @(posedge pixel_clk) begin\n        request_meta <= publish_toggle;")
READER_RESET_BEFORE = b"        if (reset) begin\n            read_page <= 2;"
READER_RESET_AFTER = b"        if (reset_pixel) begin\n            read_page <= 2;"

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
    """Swap the linked ROM lanes for the uploaded firmware RAM and its write port."""
    if b"MISTRAL" in source or b"c64_rom " in source.replace(ROM_BEFORE, b""):
        raise ValueError("C64 machine ROM adaptation contract changed")
    return _derive(source, ((ROM_BEFORE, ROM_AFTER, 1), (PORTS_BEFORE, PORTS_AFTER, 1)), "C64 machine ROM")


def retarget_ram(name, source):
    """Block RAM hints for this family."""
    return _derive(source, RAM_FILES[name], "C64 " + name)


def retarget_frame_store(source):
    """Inferred pages in place of the placed M10K primitives; the reader's reset synchronized."""
    if source.count(STORE_BEGIN) != 1 or source.count(STORE_END) != 1:
        raise ValueError("C64 frame store adaptation contract changed")
    start = source.index(STORE_BEGIN)
    stop = source.index(STORE_END) + len(STORE_END)
    removed = source[start:stop]
    if (stop < start or removed.count(b"MISTRAL_M10K") != 1 or b"MISTRAL" in source[:start] + source[stop:] or
            b"reg [3:0] page0 [0:104447];" not in removed):
        raise ValueError("C64 frame store adaptation contract changed")
    stored = source[:start] + STORE_AFTER + source[stop:]
    result, record = _derive(stored, ((READER_BEFORE, READER_AFTER, 1),
                                      (READER_RESET_BEFORE, READER_RESET_AFTER, 1)), "C64 frame store reader")
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
