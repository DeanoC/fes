"""DE25 derivations of the unchanged DE10 Atari ST sources."""
import hashlib

# The shared SDRAM controller forwards its clock through a Cyclone V DDR
# output cell: the pin rises on the fabric falling edge. The same relation
# here is the inverted clock itself, as the DE25 ramtest controller drives it.
CLOCK_BEGIN = b"    altddio_out #(\n"
CLOCK_END = (b"        .oe_out()\n"
             b"        /* verilator lint_on PINCONNECTEMPTY */\n"
             b"    );\n")
CLOCK_AFTER = b"    assign sdram_clk = ~clk_pin;\n"
# The ROM lives in the SDRAM above RAM and the floppy region, at halfword
# c0000. The CPU fetches it through its own memory port, so a ROM read gets the
# CPU's phase slot like a RAM read; cpu_rom selects the region.
MEMORY_PORT_BEFORE = b"    input wire [18:1] cpu_addr,\n"
MEMORY_PORT_AFTER = b"    input wire [18:1] cpu_addr,\n    input wire cpu_rom,\n"
MEMORY_MAP_BEFORE = b"            offer_addr = {8'd0, cpu_addr};\n"
MEMORY_MAP_AFTER = b"            offer_addr = {6'd0, cpu_rom, cpu_rom, cpu_addr};\n"


def retarget_memory(source):
    """Give st_memory's CPU port the ROM region; nothing else changes."""
    if (source.count(MEMORY_PORT_BEFORE) != 1 or source.count(MEMORY_MAP_BEFORE) != 1 or
            source.count(b"cpu_addr") != 2 or b"cpu_rom" in source or
            b"// Physical words 00000-3ffff hold 512 KiB ST RAM; 40000-a67ff hold the\n" not in source):
        raise ValueError("ST memory ROM region adaptation contract changed")
    result = source.replace(MEMORY_PORT_BEFORE, MEMORY_PORT_AFTER).replace(MEMORY_MAP_BEFORE, MEMORY_MAP_AFTER)
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "changes": [{"before": MEMORY_PORT_BEFORE.decode(), "after": MEMORY_PORT_AFTER.decode()},
                                {"before": MEMORY_MAP_BEFORE.decode(), "after": MEMORY_MAP_AFTER.decode()}]}


TRANSPORT_MARKER = b"// Atomic register transport shared by the bring-up and standard-core wrappers.\n"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def retarget_sdram(source):
    """Replace only the forwarded-clock DDR cell of sdram_addon_port."""
    if source.count(CLOCK_BEGIN) != 1 or source.count(CLOCK_END) != 1:
        raise ValueError("SDRAM controller clock adaptation contract changed")
    begin = source.index(CLOCK_BEGIN)
    end = source.index(CLOCK_END) + len(CLOCK_END)
    removed = source[begin:end]
    if (end < begin or b'.intended_device_family("Cyclone V")' not in removed or
            b".outclock(clk_pin)" not in removed or b".dataout(sdram_clk)" not in removed or
            removed.count(b"altddio_out") != 1 or b"altddio" in source[:begin] + source[end:]):
        raise ValueError("SDRAM controller clock adaptation contract changed")
    result = source[:begin] + CLOCK_AFTER + source[end:]
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "removed_sha256": sha(removed), "after": CLOCK_AFTER.decode()}


CACHE_BEFORE = b'(* ramstyle = "M10K" *) reg [15:0] words [0:80];'
CACHE_AFTER = b'(* ramstyle = "M20K" *) reg [15:0] words [0:80];'
# The DE10 frame store is 234 hand-chunked 1024x9 memories with a grouped read
# mux, shaped for M10K blocks and written from one case statement. Quartus
# builds that write from two million registers, and one M20K per chunk needs
# 234 blocks. Each bank becomes one memory here, for Quartus to shape: the
# same words at the same addresses, one statement per bank write, and the
# original two pixel edges from address to output_rgb. A read past the last
# chunk, a reset and every bank but the front one still give zero.
FRAME_WORDS = 78 * 1024
STORE_BEGIN = b"    // Explicit shallow chunks avoid the mapper selecting ninety 8192x1\n"
STORE_KEEP = b"    reg [1:0] front_bank;\n"
MUX_BEGIN = b"    // Register local 16-chunk groups before the final OR. With the RAM read,\n"
MUX_END = b"        assign group_read2[g] = selected2;\n    end endgenerate\n"
OUTPUT_BEFORE = (
    b"    assign output_rgb = group_read0[0] | group_read0[1] | group_read0[2] | group_read0[3] | group_read0[4] |\n"
    b"                        group_read1[0] | group_read1[1] | group_read1[2] | group_read1[3] | group_read1[4] |\n"
    b"                        group_read2[0] | group_read2[1] | group_read2[2] | group_read2[3] | group_read2[4];\n")
CHUNKS_BEGIN = b"    generate for (genvar chunk = 0; chunk < FRAME_CHUNKS; chunk = chunk + 1) begin : frame_chunks\n"
CHUNKS_END = b"                endcase\n            end\n        end\n    end endgenerate\n"
STORE_AFTER = b"""    // One memory per bank (DE25 derivation); see the read and write below.
    localparam integer FRAME_WORDS = 79872;
    (* ramstyle = "M20K" *) reg [8:0] frame0 [0:FRAME_WORDS-1];
    (* ramstyle = "M20K" *) reg [8:0] frame1 [0:FRAME_WORDS-1];
    (* ramstyle = "M20K" *) reg [8:0] frame2 [0:FRAME_WORDS-1];
"""
MUX_AFTER = b"""    // One RAM edge and one selection edge, as the grouped DE10 read. The word
    // is tagged with the front bank at the RAM edge.
    reg [8:0] frame_word0, frame_word1, frame_word2, frame_selected;
    reg [1:0] frame_read_bank;
    reg frame_read_hit;
    wire frame_read_present = output_address < 17'(FRAME_WORDS);
    always @(posedge clk_pixel) begin
        frame_word0 <= frame0[frame_read_present ? output_address : 17'd0];
        frame_word1 <= frame1[frame_read_present ? output_address : 17'd0];
        frame_word2 <= frame2[frame_read_present ? output_address : 17'd0];
        frame_read_hit <= frame_read_present;
        frame_read_bank <= front_bank;
        frame_selected <= reset_pixel || !frame_read_hit ? 9'd0 :
            frame_read_bank[1] ? frame_word2 : frame_read_bank[0] ? frame_word1 : frame_word0;
    end
"""
OUTPUT_AFTER = b"    assign output_rgb = frame_selected;\n"
CHUNKS_AFTER = b"""    wire frame_store_write = frame_write && !reset_sys && !lookup_hold &&
                             frame_write_address < 17'(FRAME_WORDS);
    wire [8:0] frame_store_data = frame_write_black ? 9'd0 : frame_write_rgb;
    always @(posedge clk_sys)
        if (frame_store_write && frame_write_bank == 2'd0) frame0[frame_write_address] <= frame_store_data;
    always @(posedge clk_sys)
        if (frame_store_write && frame_write_bank == 2'd1) frame1[frame_write_address] <= frame_store_data;
    always @(posedge clk_sys)
        if (frame_store_write && frame_write_bank == 2'd2) frame2[frame_write_address] <= frame_store_data;
"""


def _region(source, begin, end, after, label):
    if source.count(begin) != 1 or source.count(end) < 1:
        raise ValueError("ST native video " + label + " adaptation contract changed")
    start = source.index(begin)
    stop = source.index(end, start) + len(end)
    return source[:start] + after + source[stop:], source[start:stop]


def retarget_native_video(source):
    """The line cache's block hint and one inferable memory per frame bank."""
    if source.count(CACHE_BEFORE) != 1 or source.count(b"ramstyle") != 1 or source.count(OUTPUT_BEFORE) != 1:
        raise ValueError("ST native video adaptation contract changed")
    result = source.replace(CACHE_BEFORE, CACHE_AFTER).replace(OUTPUT_BEFORE, OUTPUT_AFTER)
    removed = {}
    # The chunk declarations end where the bank selector is declared.
    start = result.index(STORE_BEGIN) if result.count(STORE_BEGIN) == 1 else -1
    stop = result.index(STORE_KEEP) if result.count(STORE_KEEP) == 1 else -1
    if start < 0 or stop < start:
        raise ValueError("ST native video store adaptation contract changed")
    removed["store"] = result[start:stop]
    if b"    localparam integer FRAME_CHUNKS = 78;\n" not in removed["store"]:
        raise ValueError("ST native video frame size changed")
    result = result[:start] + STORE_AFTER + result[stop:]
    result, removed["mux"] = _region(result, MUX_BEGIN, MUX_END, MUX_AFTER, "read mux")
    result, removed["chunks"] = _region(result, CHUNKS_BEGIN, CHUNKS_END, CHUNKS_AFTER, "chunk")
    for name in (b"FRAME_CHUNKS", b"chunk_read", b"group_read", b"frame_chunks"):
        if name in result:
            raise ValueError("ST native video adaptation left " + name.decode())
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "removed_sha256": {name: sha(data) for name, data in removed.items()},
                    "frame_words": FRAME_WORDS}


def transport(source):
    """Only the shared atomic transport from the ZX81 computer file, unchanged."""
    if source.count(TRANSPORT_MARKER) != 1:
        raise ValueError("DE25 computer transport boundary changed")
    result = (b'`include "fes_computer.vh"\n`include "generated/fes_de25_computer.vh"\n' +
              source[source.index(TRANSPORT_MARKER):])
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "selection": "fes_de25_computer_transport and its two includes"}
