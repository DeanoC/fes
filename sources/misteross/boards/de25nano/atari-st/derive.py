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
# The frame store writes one of three banks from a case statement in one
# clocked block. Yosys infers three dual-clock memories from it; Quartus builds
# two million registers. The same writes as one plain statement per bank infer
# block RAM. Condition, address and data are unchanged.
FRAME_BEFORE = b"""        always @(posedge clk_sys) begin
            if (frame_write && !reset_sys && !lookup_hold && frame_write_address[16:10] == 7'(chunk)) begin
                case (frame_write_bank)
                    2'd0: frame0[frame_write_address[9:0]] <= frame_write_black ? 9'd0 : frame_write_rgb;
                    2'd1: frame1[frame_write_address[9:0]] <= frame_write_black ? 9'd0 : frame_write_rgb;
                    2'd2: frame2[frame_write_address[9:0]] <= frame_write_black ? 9'd0 : frame_write_rgb;
                    default: ;
                endcase
            end
        end
"""
FRAME_AFTER = b"""        wire chunk_write = frame_write && !reset_sys && !lookup_hold && frame_write_address[16:10] == 7'(chunk);
        wire [8:0] chunk_data = frame_write_black ? 9'd0 : frame_write_rgb;
        always @(posedge clk_sys)
            if (chunk_write && frame_write_bank == 2'd0) frame0[frame_write_address[9:0]] <= chunk_data;
        always @(posedge clk_sys)
            if (chunk_write && frame_write_bank == 2'd1) frame1[frame_write_address[9:0]] <= chunk_data;
        always @(posedge clk_sys)
            if (chunk_write && frame_write_bank == 2'd2) frame2[frame_write_address[9:0]] <= chunk_data;
"""


def retarget_native_video(source):
    """The line cache's block hint and the frame store's write form, for Quartus inference."""
    if (source.count(CACHE_BEFORE) != 1 or source.count(b'ramstyle') != 1 or
            source.count(FRAME_BEFORE) != 1 or source.count(b"frame_write_bank)") != 1):
        raise ValueError("ST native video adaptation contract changed")
    result = source.replace(CACHE_BEFORE, CACHE_AFTER).replace(FRAME_BEFORE, FRAME_AFTER)
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "changes": [{"before": CACHE_BEFORE.decode(), "after": CACHE_AFTER.decode()},
                                {"before": FRAME_BEFORE.decode(), "after": FRAME_AFTER.decode()}]}


def transport(source):
    """Only the shared atomic transport from the ZX81 computer file, unchanged."""
    if source.count(TRANSPORT_MARKER) != 1:
        raise ValueError("DE25 computer transport boundary changed")
    result = (b'`include "fes_computer.vh"\n`include "generated/fes_de25_computer.vh"\n' +
              source[source.index(TRANSPORT_MARKER):])
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "selection": "fes_de25_computer_transport and its two includes"}
