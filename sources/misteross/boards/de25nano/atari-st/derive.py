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
# 81 words fill a twentieth of an M20K; the line caches are LUT RAM here.
CACHE_AFTER = b'(* ramstyle = "MLAB" *) reg [15:0] words [0:80];'
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


def _edits(source, edits, label):
    """Apply exact single-occurrence replacements, in order; refuse any drift."""
    result, changes = source, []
    for before, after in edits:
        if result.count(before) != 1:
            raise ValueError(label + " adaptation contract changed: " + before.decode().strip().splitlines()[0])
        result = result.replace(before, after)
        changes.append({"before": before.decode(), "after": after.decode()})
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result), "changes": changes}


# The DE10 video crosses between the machine and pixel clocks with toggles and
# bundles held across each round trip, and the receiving side reads each bundle
# through its own logic. That is sound, but signoff refuses logic on a crossing
# path and a vector of independent bits through per-bit synchronizers. Here:
#   - every toggle and single-bit level leaves a launch register of its own
#     and crosses its own two-stage synchronizer, which runs free of its
#     clock's synchronous reset;
#   - every wider bundle is copied into a receiving-clock register when its
#     toggle arrives, with nothing on the path but that register's enable,
#     and the logic reads the copy one receiving clock later.
# A level and its toggle change on one launch edge and cross the same number
# of stages; the toggle is then acted on one clock later, after the level.
ADAPTER_EDITS = (
    # The configuration snapshot is copied when its acknowledge arrives and
    # used one pixel clock later. There are two copies: an arriving snapshot
    # lands in the one not in use, and the frame end that applied the pending
    # configuration now switches to it. The active configuration is that
    # selection, so no register sits bare behind a copy, which signoff would
    # read as a second synchronizer.
    (b"    reg [23:0] active_base;\n"
     b"    reg [1:0] active_resolution;\n"
     b"    reg [143:0] active_palette;\n",
     b"    wire [23:0] active_base;\n"
     b"    wire [1:0] active_resolution;\n"
     b"    wire [143:0] active_palette;\n"),
    (b"    wire config_complete = config_waiting && config_ack_sync == config_request;\n",
     b"    wire config_arrived = config_waiting && config_ack_sync == config_request;\n"
     b"    reg config_complete, active_slot;\n"
     b"    reg [169:0] config_copy0, config_copy1;\n"
     b"    assign {active_base, active_resolution, active_palette} = active_slot ? config_copy1 : config_copy0;\n"),
    (b"            active_base <= 24'd0;\n"
     b"            active_resolution <= 2'd0;\n"
     b"            active_palette <= 144'd0;\n",
     b"            active_slot <= 1'b0;\n"),
    # 80 words fill a twentieth of an M20K; the line caches are LUT RAM here.
    (b"    reg [15:0] cache0 [0:79];\n    reg [15:0] cache1 [0:79];\n",
     b'    (* ramstyle = "MLAB" *) reg [15:0] cache0 [0:79];\n'
     b'    (* ramstyle = "MLAB" *) reg [15:0] cache1 [0:79];\n'),
    (b'    (* async_reg = "true" *) reg [1:0] job_req_meta, job_req_sync;\n'
     b'    (* async_reg = "true" *) reg [1:0] job_done_meta, job_done_sync;\n',
     b"    reg job_request_launch0, job_request_launch1, job_done_launch0, job_done_launch1;\n"
     b"    reg job_ok_launch0, job_ok_launch1, job_high_launch0, job_high_launch1;\n"
     b'    (* async_reg = "true" *) reg job_req_meta0, job_req_sync0, job_req_meta1, job_req_sync1;\n'
     b'    (* async_reg = "true" *) reg job_done_meta0, job_done_sync0, job_done_meta1, job_done_sync1;\n'
     b'    (* async_reg = "true" *) reg job_ok_meta0, job_ok_pixel0, job_ok_meta1, job_ok_pixel1;\n'
     b'    (* async_reg = "true" *) reg job_high_meta0, job_high_sys0, job_high_meta1, job_high_sys1;\n'
     b"    wire [1:0] job_req_sync = {job_req_sync1, job_req_sync0};\n"
     b"    wire [1:0] job_done_sync = {job_done_sync1, job_done_sync0};\n"
     b"    wire [1:0] job_ok_pixel = {job_ok_pixel1, job_ok_pixel0};\n"
     b"    wire [1:0] job_high_sys = {job_high_sys1, job_high_sys0};\n"),
    (b"    reg [1:0] job_start_ok;\n",
     b"    reg [1:0] job_start_ok;\n"
     b"    reg [1:0] job_taken, job_start_ok_sys, job_done_taken;\n"
     b"    reg [17:0] job_start_sys [0:1];\n"),
    (b"    wire pending0 = job_req_sync[0] != job_done[0];\n"
     b"    wire pending1 = job_req_sync[1] != job_done[1];\n",
     b"    wire pending0 = job_taken[0] != job_done[0];\n"
     b"    wire pending1 = job_taken[1] != job_done[1];\n"),
    (b"    wire [6:0] selected_words = job_high[selected_bank] ? 7'd40 : 7'd80;\n",
     b"    wire [6:0] selected_words = job_high_sys[selected_bank] ? 7'd40 : 7'd80;\n"),
    (b"    always @(posedge clk_sys) begin\n"
     b"        if (reset_sys) begin\n"
     b"            config_req_meta <= 1'b0;\n"
     b"            config_req_sync <= 1'b0;\n"
     b"            config_ack <= 1'b0;\n",
     b"    always @(posedge clk_sys) begin\n"
     b"        config_req_meta <= config_request;\n"
     b"        config_req_sync <= config_req_meta;\n"
     b"        job_req_meta0 <= job_request_launch0;\n"
     b"        job_req_sync0 <= job_req_meta0;\n"
     b"        job_req_meta1 <= job_request_launch1;\n"
     b"        job_req_sync1 <= job_req_meta1;\n"
     b"        job_high_meta0 <= job_high_launch0;\n"
     b"        job_high_sys0 <= job_high_meta0;\n"
     b"        job_high_meta1 <= job_high_launch1;\n"
     b"        job_high_sys1 <= job_high_meta1;\n"
     b"        job_done_launch0 <= job_done[0];\n"
     b"        job_done_launch1 <= job_done[1];\n"
     b"        job_ok_launch0 <= job_ok[0];\n"
     b"        job_ok_launch1 <= job_ok[1];\n"
     b"    end\n"
     b"    always @(posedge clk_sys) begin\n"
     b"        if (reset_sys) begin\n"
     b"            job_taken <= 2'd0;\n"
     b"            config_ack <= 1'b0;\n"),
    (b"            job_req_meta <= 2'd0;\n"
     b"            job_req_sync <= 2'd0;\n"
     b"            job_done <= 2'd0;\n",
     b"            job_done <= 2'd0;\n"),
    (b"            config_req_meta <= config_request;\n"
     b"            config_req_sync <= config_req_meta;\n"
     b"            job_req_meta <= job_request;\n"
     b"            job_req_sync <= job_req_meta;\n",
     b"            job_taken <= job_req_sync;\n"
     b"            if (job_req_sync[0] != job_taken[0]) begin\n"
     b"                job_start_sys[0] <= job_start[0][17:0];\n"
     b"                job_start_ok_sys[0] <= job_start_ok[0];\n"
     b"            end\n"
     b"            if (job_req_sync[1] != job_taken[1]) begin\n"
     b"                job_start_sys[1] <= job_start[1][17:0];\n"
     b"                job_start_ok_sys[1] <= job_start_ok[1];\n"
     b"            end\n"),
    (b"                    memory_token <= job_req_sync[selected_bank];\n",
     b"                    memory_token <= job_taken[selected_bank];\n"),
    (b"                    if (job_start_ok[selected_bank]) begin\n"
     b"                        legacy_addr <= job_start[selected_bank][17:0];\n",
     b"                    if (job_start_ok_sys[selected_bank]) begin\n"
     b"                        legacy_addr <= job_start_sys[selected_bank];\n"),
    (b"                        job_done[selected_bank] <= job_req_sync[selected_bank];\n",
     b"                        job_done[selected_bank] <= job_taken[selected_bank];\n"),
    (b"    always @(posedge clk_pixel) begin\n"
     b"        if (reset_pixel) begin\n"
     b"            horizontal <= 11'd0;\n"
     b"            vertical <= 10'd0;\n"
     b"            hold_meta <= 1'b0;\n"
     b"            hold_sync <= 1'b0;\n"
     b"            config_ack_meta <= 1'b0;\n"
     b"            config_ack_sync <= 1'b0;\n",
     b"    always @(posedge clk_pixel) begin\n"
     b"        hold_meta <= hold;\n"
     b"        hold_sync <= hold_meta;\n"
     b"        config_ack_meta <= config_ack;\n"
     b"        config_ack_sync <= config_ack_meta;\n"
     b"        job_done_meta0 <= job_done_launch0;\n"
     b"        job_done_sync0 <= job_done_meta0;\n"
     b"        job_done_meta1 <= job_done_launch1;\n"
     b"        job_done_sync1 <= job_done_meta1;\n"
     b"        job_ok_meta0 <= job_ok_launch0;\n"
     b"        job_ok_pixel0 <= job_ok_meta0;\n"
     b"        job_ok_meta1 <= job_ok_launch1;\n"
     b"        job_ok_pixel1 <= job_ok_meta1;\n"
     b"        job_request_launch0 <= job_request[0];\n"
     b"        job_request_launch1 <= job_request[1];\n"
     b"        job_high_launch0 <= job_high[0];\n"
     b"        job_high_launch1 <= job_high[1];\n"
     b"    end\n"
     b"    always @(posedge clk_pixel) begin\n"
     b"        if (reset_pixel) begin\n"
     b"            horizontal <= 11'd0;\n"
     b"            vertical <= 10'd0;\n"
     b"            config_complete <= 1'b0;\n"
     b"            job_done_taken <= 2'd0;\n"),
    (b"            job_done_meta <= 2'd0;\n"
     b"            job_done_sync <= 2'd0;\n"
     b"            job_request <= 2'd0;\n",
     b"            job_request <= 2'd0;\n"),
    (b"            hold_meta <= hold;\n"
     b"            hold_sync <= hold_meta;\n"
     b"            config_ack_meta <= config_ack;\n"
     b"            config_ack_sync <= config_ack_meta;\n"
     b"            job_done_meta <= job_done;\n"
     b"            job_done_sync <= job_done_meta;\n",
     b"            job_done_taken <= job_done_sync;\n"
     b"            config_complete <= config_arrived && !config_complete;\n"
     b"            if (config_arrived && !config_complete && active_slot) config_copy0 <= config_bundle_sys;\n"
     b"            if (config_arrived && !config_complete && !active_slot) config_copy1 <= config_bundle_sys;\n"),
    (b"                pending_config <= config_bundle_sys;\n", b""),
    (b"    reg [169:0] pending_config;\n", b""),
    (b"            pending_config <= 170'd0;\n", b""),
    (b"                if (cache_busy[bank] && job_done_sync[bank] == job_request[bank]) begin\n"
     b"                    cache_busy[bank] <= 1'b0;\n"
     b"                    cache_valid[bank] <= job_ok[bank] && job_current[bank];\n",
     b"                if (cache_busy[bank] && job_done_taken[bank] == job_request[bank]) begin\n"
     b"                    cache_busy[bank] <= 1'b0;\n"
     b"                    cache_valid[bank] <= job_ok_pixel[bank] && job_current[bank];\n"),
    (b"                        {active_base, active_resolution, active_palette} <=\n"
     b"                            config_complete ? config_bundle_sys : pending_config;\n",
     b"                        active_slot <= !active_slot;\n"),
)


def retarget_video_adapter(source):
    """Give every crossing one launch register and one receiving-clock landing."""
    return _edits(source, ADAPTER_EDITS, "ST video adapter crossing")


NATIVE_CROSSING_EDITS = (
    (b'    (* async_reg = "true" *) reg [2:0] pub_meta, pub_sync, release_meta, release_sync;\n',
     b"    reg published_launch0, published_launch1, published_launch2;\n"
     b"    reg released_launch0, released_launch1, released_launch2;\n"
     b'    (* async_reg = "true" *) reg pub_meta0, pub_sync0, pub_meta1, pub_sync1, pub_meta2, pub_sync2;\n'
     b'    (* async_reg = "true" *) reg release_meta0, release_sync0, release_meta1, release_sync1;\n'
     b'    (* async_reg = "true" *) reg release_meta2, release_sync2;\n'
     b"    wire [2:0] pub_sync = {pub_sync2, pub_sync1, pub_sync0};\n"
     b"    wire [2:0] release_sync = {release_sync2, release_sync1, release_sync0};\n"
     b"    reg [2:0] pub_taken, pal_pixel;\n"
     b"    reg [2:0] publication_pixel [0:2];\n"
     b"    reg [8:0] border_pixel [0:2];\n"
     b"    reg [8:0] height_pixel [0:2];\n"),
    (b"    wire [2:0] pending_publications = pub_sync ^ seen;\n"
     b"    wire bank1_newer0 = newer_publication(publication_number[1], publication_number[0]);\n"
     b"    wire bank2_newer0 = newer_publication(publication_number[2], publication_number[0]);\n"
     b"    wire bank2_newer1 = newer_publication(publication_number[2], publication_number[1]);\n",
     b"    wire [2:0] pending_publications = pub_taken ^ seen;\n"
     b"    wire bank1_newer0 = newer_publication(publication_pixel[1], publication_pixel[0]);\n"
     b"    wire bank2_newer0 = newer_publication(publication_pixel[2], publication_pixel[0]);\n"
     b"    wire bank2_newer1 = newer_publication(publication_pixel[2], publication_pixel[1]);\n"),
    (b"    always @(posedge clk_pixel) begin\n"
     b"        if (reset_pixel) begin\n"
     b"            pub_meta <= 3'd0; pub_sync <= 3'd0;\n",
     b"    always @(posedge clk_pixel) begin\n"
     b"        pub_meta0 <= published_launch0; pub_sync0 <= pub_meta0;\n"
     b"        pub_meta1 <= published_launch1; pub_sync1 <= pub_meta1;\n"
     b"        pub_meta2 <= published_launch2; pub_sync2 <= pub_meta2;\n"
     b"        released_launch0 <= released[0];\n"
     b"        released_launch1 <= released[1];\n"
     b"        released_launch2 <= released[2];\n"
     b"    end\n"
     b"    always @(posedge clk_sys) begin\n"
     b"        release_meta0 <= released_launch0; release_sync0 <= release_meta0;\n"
     b"        release_meta1 <= released_launch1; release_sync1 <= release_meta1;\n"
     b"        release_meta2 <= released_launch2; release_sync2 <= release_meta2;\n"
     b"        published_launch0 <= published[0];\n"
     b"        published_launch1 <= published[1];\n"
     b"        published_launch2 <= published[2];\n"
     b"    end\n"
     b"    always @(posedge clk_pixel) begin\n"
     b"        if (reset_pixel) begin\n"
     b"            pub_taken <= 3'd0;\n"),
    (b"            pub_meta <= published; pub_sync <= pub_meta;\n",
     b"            pub_taken <= pub_sync;\n"
     b"            for (candidate = 0; candidate < 3; candidate = candidate + 1)\n"
     b"                if (pub_sync[candidate] != pub_taken[candidate]) begin\n"
     b"                    publication_pixel[candidate] <= publication_number[candidate];\n"
     b"                    border_pixel[candidate] <= border[candidate];\n"
     b"                    height_pixel[candidate] <= height[candidate];\n"
     b"                    pal_pixel[candidate] <= pal_banks[candidate];\n"
     b"                end\n"),
    (b"                    if (2'(candidate) != choose_bank) released[candidate] <= pub_sync[candidate];\n"
     b"                    seen[candidate] <= pub_sync[candidate];\n",
     b"                    if (2'(candidate) != choose_bank) released[candidate] <= pub_taken[candidate];\n"
     b"                    seen[candidate] <= pub_taken[candidate];\n"),
    (b"                front_border <= border[choose_bank];\n"
     b"                output_height <= height[choose_bank];\n"
     b"                output_pal <= pal_banks[choose_bank];\n",
     b"                front_border <= border_pixel[choose_bank];\n"
     b"                output_height <= height_pixel[choose_bank];\n"
     b"                output_pal <= pal_pixel[choose_bank];\n"),
    (b"            release_meta <= 3'd0; release_sync <= 3'd0; published <= 3'd0;\n",
     b"            published <= 3'd0;\n"),
    (b"            release_meta <= released; release_sync <= release_meta;\n", b""),
)


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
    result, crossing = _edits(result, NATIVE_CROSSING_EDITS, "ST native video crossing")
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "removed_sha256": {name: sha(data) for name, data in removed.items()},
                    "frame_words": FRAME_WORDS, "crossing_changes": crossing["changes"]}


def transport(source):
    """Only the shared atomic transport from the ZX81 computer file, unchanged."""
    if source.count(TRANSPORT_MARKER) != 1:
        raise ValueError("DE25 computer transport boundary changed")
    result = (b'`include "fes_computer.vh"\n`include "generated/fes_de25_computer.vh"\n' +
              source[source.index(TRANSPORT_MARKER):])
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "selection": "fes_de25_computer_transport and its two includes"}
