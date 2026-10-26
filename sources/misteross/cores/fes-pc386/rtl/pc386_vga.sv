// SPDX-License-Identifier: MIT
// pc386 register-level VGA (docs/pc386-machine-design.md, "VGA").
//
// CPU side: VGA port bytes from the I/O decoder and MMIO dwords for
// A0000h-BFFFFh share one FIFO, so a posted VRAM write followed by OUT 3C5h is
// planar-processed with the old map mask. Port writes and MMIO writes post;
// port reads and MMIO reads wait until the FIFO and the write coalescer have
// drained. The planar engine turns each MMIO dword into VRAM word writes, one
// per clock: bytes that land at the same planar offset (a chain-4 dword, an
// odd/even pair) share a word. VRAM is 65,536 words of 32 bits, one byte per
// plane, in HPS DDR on port 1; a four-line coalescer of 32-byte lines turns
// word writes into four-beat bursts.
//
// Address translation (CPU address A within the memory map window):
//   chain-4 (sequencer 4 bit 3)   plane A[1:0] at offset A >> 2
//   odd/even (sequencer 4 bit 2 clear)  planes A[0] ? 1,3 : 0,2 at offset A & ~1
//   planar                        every plane at offset A
// The map mask gates the planes in every mode. Reads use the same translation;
// read mode 0 returns the plane A selects (chain-4), the read map select with
// A[0] (odd/even) or the read map select, and every read loads the latches.
//
// The scanout (pc386_vga_scanout) shares port 1 for line prefetch and has
// priority over the CPU side; it owns the DAC and the capture writer on port 2.
// Port addresses are 64-bit word addresses: VRAM_BASE is the word address of
// window offset 00800000h, CAPTURE_BASE of 00A00000h.
module pc386_vga #(
    parameter [28:0] VRAM_BASE = 29'h06100000,
    parameter [28:0] CAPTURE_BASE = 29'h06140000,
    parameter integer DOT0_N = 1007, DOT0_D = 4000,      // 25.175 MHz at 100 MHz
    parameter integer DOT1_N = 14161, DOT1_D = 50000     // 28.322 MHz at 100 MHz
) (
    input wire clk, reset,
    // VGA ports: one byte per handshake, valid held until done
    input wire io_valid, io_write,
    input wire [15:0] io_port,
    input wire [7:0] io_wdata,
    output reg io_done,
    output reg [7:0] io_rdata,
    // MMIO: dword offsets within A0000h-BFFFFh. Writes post; a read is taken
    // once and answered with rvalid.
    input wire mem_valid, mem_write,
    input wire [16:2] mem_address,
    input wire [3:0] mem_byteenable,
    input wire [31:0] mem_wdata,
    output wire mem_ready,
    output reg mem_rvalid,
    output reg [31:0] mem_rdata,
    // DDR port 1: VRAM
    output reg [28:0] p1_address,
    output reg [7:0] p1_burstcount,
    output reg p1_read, p1_write,
    output wire [63:0] p1_writedata,
    output wire [7:0] p1_byteenable,
    input wire p1_waitrequest,
    input wire [63:0] p1_readdata,
    input wire p1_readdatavalid,
    // DDR port 2: capture writes
    output wire [28:0] cap_address,
    output wire [7:0] cap_burstcount,
    output wire cap_write,
    output wire [63:0] cap_writedata,
    input wire cap_waitrequest,
    // Completed capture buffers, for the presenter
    output wire [1:0] frame_buffer,
    output wire [9:0] frame_width, frame_height,
    output wire [15:0] frame_sequence,
    input wire [1:0] present_buffer,
    // Trace and counters
    output wire frame_start, frame_done, start_latched,
    output wire [15:0] frame_count,
    output wire [31:0] scanout_underflows, capture_drops
);
    // ---- Registers -----------------------------------------------------------
    reg [7:0] misc, feature;
    reg video_enable;
    reg [7:0] seq_index;
    reg [1:0] seq_reset;
    reg [7:0] seq_clocking;
    reg [3:0] map_mask;
    reg [5:0] char_map;
    reg seq_extended, odd_even_disable, chain_four;
    reg [7:0] gc_index;
    reg [3:0] set_reset, enable_set_reset, color_compare, color_dont_care;
    reg [2:0] rotate;
    reg [1:0] raster_op, read_map, write_mode, shift_mode, memory_map;
    reg read_mode, gc_odd_even, graphics, chain_odd_even;
    reg [7:0] bit_mask;
    reg [5:0] crtc_index;
    reg [7:0] crtc [0:24];
    reg [4:0] attr_index;
    reg attr_pas, attr_flip;
    reg [7:0] palette [0:15];
    reg [7:0] attr_mode;     // bit 4 is not stored
    reg [5:0] overscan;
    reg [3:0] plane_enable, pel_pan, color_select;
    reg [7:0] pel_mask, dac_read_index, dac_write_index;
    reg [1:0] dac_read_cycle, dac_write_cycle, dac_state;
    reg [5:0] dac_red, dac_green;
    reg [31:0] latch;

    // ---- Front FIFO --------------------------------------------------------------
    // {io, address[16:2] or port, byte enables, data}
    localparam integer ENTRY = 1 + 15 + 4 + 32;
    wire fifo_full, fifo_empty;
    wire [4:0] fifo_count;
    wire [ENTRY-1:0] head;
    reg fifo_push, fifo_pop;
    reg [ENTRY-1:0] fifo_in;
    pc386_fifo #(.WIDTH(ENTRY), .DEPTH(16)) fifo (
        .clk(clk), .reset(reset), .push(fifo_push), .din(fifo_in), .full(fifo_full),
        .pop(fifo_pop), .dout(head), .empty(fifo_empty), .count(fifo_count)
    );
    wire head_io = head[ENTRY-1];
    wire [14:0] head_address = head[ENTRY-2 -: 15];
    wire [3:0] head_enables = head[35:32];
    wire [31:0] head_data = head[31:0];

    // ---- CPU request intake ---------------------------------------------------
    // A read (port or memory) parks here until the FIFO and coalescer drain.
    reg io_taken;
    reg read_pending, read_is_io;
    reg [15:0] read_port;
    reg [14:0] read_address;
    reg [3:0] read_enables;
    wire io_new = io_valid && !io_taken && !read_pending;
    wire io_write_take = io_new && io_write && !fifo_full;
    wire mem_write_take = mem_valid && mem_write && !fifo_full && !read_pending && !io_write_take;
    wire mem_read_take = mem_valid && !mem_write && !read_pending && !io_new;
    assign mem_ready = mem_write ? mem_write_take : mem_read_take;
    always @* begin
        fifo_push = io_write_take || mem_write_take;
        fifo_in = io_write_take ? {1'b1, 5'b0, io_port[9:0], 4'b0001, 24'b0, io_wdata}
                                : {1'b0, mem_address, mem_byteenable, mem_wdata};
    end

    // ---- Planar translation --------------------------------------------------
    // A byte's planar offset from its offset in A0000h-BFFFFh, and in bit 0
    // whether it is inside the memory map and VRAM.
    function automatic [16:0] translate(input [16:0] a, input [1:0] map, input chain4, input odd_even);
        reg in_map;
        reg [16:0] offset;
        begin
            case (map)
                2'd0: begin in_map = 1'b1; offset = a; end
                2'd1: begin in_map = !a[16]; offset = {1'b0, a[15:0]}; end
                2'd2: begin in_map = a[16:15] == 2'b10; offset = {2'b0, a[14:0]}; end
                default: begin in_map = a[16:15] == 2'b11; offset = {2'b0, a[14:0]}; end
            endcase
            if (chain4) offset = {2'b0, offset[16:2]};
            else if (odd_even) offset = {offset[16:1], 1'b0};
            translate = {offset[15:0], in_map && !offset[16]};
        end
    endfunction

    reg [15:0] byte_offset [0:3];
    reg [3:0] byte_planes [0:3];
    reg [3:0] byte_inside;
    reg [15:0] read_offset [0:3];
    reg [3:0] read_inside;
    integer b;
    always @* begin
        for (b = 0; b < 4; b = b + 1) begin
            {byte_offset[b], byte_inside[b]} = translate({head_address, b[1:0]}, memory_map, chain_four, !odd_even_disable);
            byte_planes[b] = chain_four ? 4'b0001 << b[1:0] :
                             !odd_even_disable ? (b[0] ? 4'b1010 : 4'b0101) : 4'b1111;
            {read_offset[b], read_inside[b]} = translate({read_address, b[1:0]}, memory_map, chain_four, !odd_even_disable);
        end
    end

    function automatic [7:0] alu(input [1:0] op, input [7:0] value, input [7:0] latched);
        case (op)
            2'd0: alu = value;
            2'd1: alu = value & latched;
            2'd2: alu = value | latched;
            default: alu = value ^ latched;
        endcase
    endfunction
    // One plane's new byte for a CPU byte under the write mode.
    function automatic [7:0] plane_value(input [1:0] mode, input [7:0] cpu, input [7:0] latched,
                                         input sr, input esr, input [2:0] rot, input [1:0] op, input [7:0] mask);
        reg [7:0] rotated, value, merge;
        begin
            rotated = (cpu >> rot) | (cpu << (4'd8 - {1'b0, rot}));
            merge = mask;
            case (mode)
                2'd0: value = esr ? {8{sr}} : rotated;
                2'd2: value = {8{cpu[0]}};   // the caller passes the plane's colour bit in bit 0
                2'd3: begin value = {8{sr}}; merge = mask & rotated; end
                default: value = latched;
            endcase
            plane_value = mode == 2'd1 ? latched : (alu(op, value, latched) & merge) | (latched & ~merge);
        end
    endfunction

    // ---- Planar engine -----------------------------------------------------------
    // Group: the first remaining enabled byte's offset and every remaining byte
    // at that offset. Each plane takes its byte from the group member that
    // targets it.
    reg [3:0] remaining;
    reg head_loaded;
    wire [3:0] live = (head_loaded ? remaining : head_enables) & byte_inside & {4{video_enable}};
    reg [1:0] first;
    reg [3:0] group;
    reg [3:0] group_planes;
    reg [7:0] plane_cpu [0:3];
    reg [31:0] group_word;
    integer g, p;
    always @* begin
        first = 0;
        for (g = 3; g >= 0; g = g - 1) if (live[g]) first = g[1:0];
        group = 0;
        group_planes = 0;
        for (p = 0; p < 4; p = p + 1) plane_cpu[p] = 0;
        for (g = 0; g < 4; g = g + 1)
            if (live[g] && byte_offset[g] == byte_offset[first]) begin
                group[g] = 1'b1;
                group_planes = group_planes | byte_planes[g];
                for (p = 0; p < 4; p = p + 1)
                    if (byte_planes[g][p]) plane_cpu[p] = head_data[8 * g +: 8];
            end
        for (p = 0; p < 4; p = p + 1)
            group_word[8 * p +: 8] = plane_value(write_mode,
                write_mode == 2'd2 ? {7'b0, plane_cpu[p][p]} : plane_cpu[p],
                latch[8 * p +: 8], set_reset[p], enable_set_reset[p], rotate, raster_op, bit_mask);
    end
    wire [3:0] group_enables = group_planes & map_mask;

    // ---- Coalescer -----------------------------------------------------------------
    // Four 32-byte lines (eight VRAM words, four DDR beats) kept as a queue in
    // allocation order; the oldest drains first. A write that hits the draining
    // line waits for it.
    reg line_valid [0:3];
    reg [12:0] line_tag [0:3];
    reg [31:0] line_mask [0:3];
    reg [1:0] line_head, line_tail;
    reg [2:0] lines_used;
    reg draining;
    reg [1:0] drain_beat;
    reg drain_prepared;
    reg [6:0] idle;
    reg flush_all;
    wire word_valid;
    wire [15:0] word_offset = byte_offset[first];
    wire [12:0] word_tag = word_offset[15:3];
    reg hit;
    reg [1:0] hit_line;
    integer l, n;   // each always block keeps its own loop variable
    always @* begin
        hit = 0;
        hit_line = 0;
        for (l = 0; l < 4; l = l + 1)
            if (line_valid[l] && line_tag[l] == word_tag) begin hit = 1; hit_line = l[1:0]; end
    end
    wire hit_draining = hit && draining && hit_line == line_tail;
    wire word_accept = word_valid && !hit_draining && (hit || lines_used != 3'd4);
    wire [1:0] word_line = hit ? hit_line : line_head;
    wire coalescer_empty = lines_used == 0 && !draining;

    // Line data: 16 entries of one beat, written a word at a time.
    wire [63:0] drain_data;
    wire drain_accept = p1_write && !p1_waitrequest;
    wire [1:0] drain_next = drain_beat + {1'b0, drain_accept};
    pc386_ram #(.WIDTH(64), .DEPTH(16), .BYTES(8)) line_data (
        .clk(clk), .write(word_accept), .waddress({word_line, word_offset[2:1]}),
        .wdata({group_word, group_word}),
        .wbyteenable(word_offset[0] ? {group_enables, 4'b0} : {4'b0, group_enables}),
        .read(1'b1), .raddress({line_tail, drain_prepared ? drain_next : 2'd0}), .rdata(drain_data)
    );

    // ---- Engine ----------------------------------------------------------------------
    localparam [2:0] E_RUN = 3'd0, E_READ_DRAIN = 3'd1, E_READ_ISSUE = 3'd2, E_READ_WAIT = 3'd3,
                     E_READ_ANSWER = 3'd4, E_IO_READ = 3'd5;
    reg [2:0] engine;
    assign word_valid = engine == E_RUN && !fifo_empty && !head_io && live != 0;
    // The head retires when its last group is written, or at once for a
    // register write or an access entirely outside the window.
    wire head_retire = engine == E_RUN && !fifo_empty &&
                       (head_io || live == 0 || (word_accept && (live & ~group) == 0));
    always @* fifo_pop = head_retire;
    wire [14:0] read_first_beat = read_offset[read_lowest][15:1];
    reg [1:0] read_lowest, read_highest;
    integer r;
    always @* begin
        read_lowest = 0; read_highest = 0;
        for (r = 3; r >= 0; r = r - 1) if (read_enables[r] && read_inside[r]) read_lowest = r[1:0];
        for (r = 0; r < 4; r = r + 1) if (read_enables[r] && read_inside[r]) read_highest = r[1:0];
    end
    wire [14:0] read_last_beat = read_offset[read_highest][15:1];
    wire [14:0] read_span = read_last_beat - read_first_beat;
    wire [2:0] read_beat_count = read_span[2:0] + 3'd1;
    reg [63:0] read_beats [0:3];
    reg [2:0] read_received;
    wire cpu_read_data;
    reg [7:0] register_value;
    wire [7:0] status;

    // ---- Port 1 arbiter ----------------------------------------------------------
    // The scanout's single-beat reads go first; a drain burst, once started,
    // finishes. Read data returns in order, tagged by owner.
    wire scan_request;
    wire [14:0] scan_beat;
    wire scan_grant = scan_request && !p1_read && !p1_write && !draining_active;
    reg draining_active;
    wire cpu_read_issue = engine == E_READ_ISSUE && !p1_read && !p1_write && !scan_request;
    wire owner_full, owner_empty, owner_scan;
    wire [4:0] owner_fill;
    wire [2:0] owner_beats;
    reg [2:0] owner_count;
    pc386_fifo #(.WIDTH(4), .DEPTH(16)) owners (
        .clk(clk), .reset(reset),
        .push(scan_grant || cpu_read_issue),
        .din(scan_grant ? 4'b1001 : {1'b0, read_beat_count}),
        .full(owner_full), .pop(p1_readdatavalid && owner_count + 3'd1 == owner_beats),
        .dout({owner_scan, owner_beats}), .empty(owner_empty), .count(owner_fill)
    );
    wire scan_response = p1_readdatavalid && owner_scan;
    assign cpu_read_data = p1_readdatavalid && !owner_scan;

    always @(posedge clk) begin
        if (reset) begin
            p1_read <= 0; p1_write <= 0; p1_address <= 0; p1_burstcount <= 0; owner_count <= 0;
            draining <= 0; draining_active <= 0; drain_beat <= 0; drain_prepared <= 0;
        end else begin
            if (p1_readdatavalid) owner_count <= owner_count + 3'd1 == owner_beats ? 3'd0 : owner_count + 3'd1;
            if (p1_read && !p1_waitrequest) p1_read <= 0;
            if (scan_grant) begin
                p1_read <= 1; p1_address <= VRAM_BASE + {14'b0, scan_beat}; p1_burstcount <= 8'd1;
            end else if (cpu_read_issue) begin
                p1_read <= 1; p1_address <= VRAM_BASE + {14'b0, read_first_beat};
                p1_burstcount <= {5'b0, read_beat_count};
            end
            // Drain: a prepare clock reads beat 0, then four beats go out.
            if (!draining && lines_used != 0 &&
                (lines_used == 3'd4 && word_valid && !hit || flush_all || idle == 7'd64)) begin
                draining <= 1;
                drain_prepared <= 0;
                drain_beat <= 0;
            end else if (draining && !draining_active && !drain_prepared) begin
                drain_prepared <= 1;
            end else if (draining && !draining_active && drain_prepared && !p1_read && !p1_write && !scan_request) begin
                draining_active <= 1;
                p1_write <= 1;
                p1_address <= VRAM_BASE + {14'b0, line_tag[line_tail], 2'b00};
                p1_burstcount <= 8'd4;
            end else if (draining_active && drain_accept) begin
                drain_beat <= drain_next;
                if (drain_beat == 2'd3) begin
                    p1_write <= 0;
                    draining_active <= 0;
                    draining <= 0;
                    drain_prepared <= 0;
                end
            end
        end
    end
    // The RAM reads drain_next, so its output is the beat on the port: it
    // moves to the next beat in the clock after one is taken.
    assign p1_writedata = drain_data;
    assign p1_byteenable = line_mask[line_tail][8 * drain_beat +: 8];
    wire drain_retire = draining_active && drain_accept && drain_beat == 2'd3;

    always @(posedge clk) begin
        if (reset) begin
            for (n = 0; n < 4; n = n + 1) begin line_valid[n] <= 0; line_tag[n] <= 0; line_mask[n] <= 0; end
            line_head <= 0; line_tail <= 0; lines_used <= 0; idle <= 0;
        end else begin
            idle <= word_accept || lines_used == 0 ? 7'd0 : idle == 7'd64 ? idle : idle + 7'd1;
            if (word_accept && !hit) begin
                line_valid[line_head] <= 1;
                line_tag[line_head] <= word_tag;
                line_mask[line_head] <= {28'b0, group_enables} << {word_offset[2:0], 2'b00};
                line_head <= line_head + 1'b1;
            end else if (word_accept)
                line_mask[hit_line] <= line_mask[hit_line] | ({28'b0, group_enables} << {word_offset[2:0], 2'b00});
            if (drain_retire) begin
                line_valid[line_tail] <= 0;
                line_mask[line_tail] <= 0;
                line_tail <= line_tail + 1'b1;
            end
            lines_used <= lines_used + {2'b0, word_accept && !hit} - {2'b0, drain_retire};
        end
    end

    // ---- Engine and register file -----------------------------------------------
    wire [9:0] head_port = head_address[9:0];
    wire mono = !misc[0];
    // Ports Bochs's VGA claims; the rest of 3B0h-3DFh reads FFh.
    function automatic claimed(input [9:0] port, input mono_mode);
        claimed = ((port == 10'h3b4 || port == 10'h3b5 || port == 10'h3ba) && mono_mode) ||
                  (port >= 10'h3c0 && port <= 10'h3cf) ||
                  ((port == 10'h3d4 || port == 10'h3d5 || port == 10'h3da) && !mono_mode);
    endfunction
    wire [7:0] head_byte = head_data[7:0];
    wire head_claimed = claimed(head_port, mono) && (video_enable || head_port == 10'h3c3);
    wire crtc_protected = crtc[17][7] && crtc_index < 6'd8;
    integer i;

    // Register read value, with Bochs's masks; the read's side effects
    // (flip-flop, DAC read index) are applied with it.
    wire [7:0] dac_read_data;
    wire [7:0] attr_read = attr_index < 5'd16 ? palette[attr_index[3:0]] :
                           attr_index == 5'h10 ? attr_mode :
                           attr_index == 5'h11 ? {2'b0, overscan} :
                           attr_index == 5'h12 ? {4'b0, plane_enable} :
                           attr_index == 5'h13 ? {4'b0, pel_pan} :
                           attr_index == 5'h14 ? {4'b0, color_select} : 8'h00;
    reg [7:0] gc_read, seq_read;
    always @* begin
        case (gc_index)
            8'd0: gc_read = {4'b0, set_reset};
            8'd1: gc_read = {4'b0, enable_set_reset};
            8'd2: gc_read = {4'b0, color_compare};
            8'd3: gc_read = {3'b0, raster_op, rotate};
            8'd4: gc_read = {6'b0, read_map};
            8'd5: gc_read = {1'b0, shift_mode, gc_odd_even, read_mode, 1'b0, write_mode};
            8'd6: gc_read = {4'b0, memory_map, chain_odd_even, graphics};
            8'd7: gc_read = {4'b0, color_dont_care};
            8'd8: gc_read = bit_mask;
            default: gc_read = 8'h00;
        endcase
        case (seq_index)
            8'd0: seq_read = {6'b0, seq_reset};
            8'd1: seq_read = seq_clocking;
            8'd2: seq_read = {4'b0, map_mask};
            8'd3: seq_read = {2'b0, char_map};
            8'd4: seq_read = {4'b0, chain_four, odd_even_disable, seq_extended, 1'b0};
            default: seq_read = 8'h00;
        endcase
        register_value = 8'hff;
        if (claimed(read_port[9:0], mono) && (video_enable || read_port[9:0] == 10'h3c3))
            case (read_port[9:0])
                10'h3c0: register_value = {2'b0, attr_pas, attr_index};
                10'h3c1: register_value = attr_read;
                10'h3c2: register_value = 8'h00;
                10'h3c3: register_value = {7'b0, video_enable};
                10'h3c4: register_value = seq_index;
                10'h3c5: register_value = seq_read;
                10'h3c6: register_value = pel_mask;
                10'h3c7: register_value = {6'b0, dac_state};
                10'h3c8: register_value = dac_write_index;
                10'h3c9: register_value = dac_state == 2'd3 ? dac_read_data : 8'h3f;
                10'h3ca: register_value = feature;
                10'h3cc: register_value = {misc[7:5], 1'b0, misc[3:0]};
                10'h3ce: register_value = gc_index;
                10'h3cf: register_value = gc_read;
                10'h3b4, 10'h3d4: register_value = {2'b0, crtc_index};
                10'h3b5, 10'h3d5: register_value = crtc_index <= 6'd24 ? crtc[crtc_index[4:0]] :
                                                   crtc_index == 6'h22 ? latch[8 * read_map +: 8] : 8'h00;
                10'h3ba, 10'h3da: register_value = status;
                default: register_value = 8'h00;
            endcase
    end

    // MMIO read result: each byte from the word at its offset. Read mode 1
    // sets a bit where every plane not in colour don't care matches its
    // colour compare bit.
    reg [31:0] read_result;
    reg [31:0] read_latch;
    reg [31:0] word_of [0:3];
    reg [14:0] beat_index;
    reg [7:0] compare_byte;
    integer k;
    always @* begin
        read_result = 32'hffffffff;
        read_latch = latch;
        compare_byte = 0;
        for (k = 0; k < 4; k = k + 1) begin
            beat_index = read_offset[k][15:1] - read_first_beat;
            word_of[k] = read_offset[k][0] ? read_beats[beat_index[1:0]][63:32] : read_beats[beat_index[1:0]][31:0];
            if (read_enables[k] && read_inside[k]) begin
                read_latch = word_of[k];
                compare_byte = ~(((word_of[k][7:0] ^ {8{color_compare[0]}}) & {8{color_dont_care[0]}}) |
                                 ((word_of[k][15:8] ^ {8{color_compare[1]}}) & {8{color_dont_care[1]}}) |
                                 ((word_of[k][23:16] ^ {8{color_compare[2]}}) & {8{color_dont_care[2]}}) |
                                 ((word_of[k][31:24] ^ {8{color_compare[3]}}) & {8{color_dont_care[3]}}));
                if (read_mode) read_result[8 * k +: 8] = compare_byte;
                else if (chain_four) read_result[8 * k +: 8] = word_of[k][8 * {k[1:0]} +: 8];
                else if (!odd_even_disable)
                    read_result[8 * k +: 8] = word_of[k][8 * {read_map[1], k[0]} +: 8];
                else read_result[8 * k +: 8] = word_of[k][8 * read_map +: 8];
            end
        end
    end

    wire dac_write = engine == E_RUN && !fifo_empty && head_io && head_claimed &&
                     head_port == 10'h3c9 && dac_write_cycle == 2'd2;
    always @(posedge clk) begin
        io_done <= 0;
        mem_rvalid <= 0;
        if (!io_valid) io_taken <= 0;
        if (reset) begin
            engine <= E_RUN; io_taken <= 0; read_pending <= 0; read_is_io <= 0;
            read_port <= 0; read_address <= 0; read_enables <= 0; read_received <= 0;
            io_rdata <= 0; mem_rdata <= 0; remaining <= 0; head_loaded <= 0; flush_all <= 0;
            misc <= 8'hc3; feature <= 0; video_enable <= 1;
            seq_index <= 0; seq_reset <= 2'b11; seq_clocking <= 0; map_mask <= 0; char_map <= 0;
            seq_extended <= 1; odd_even_disable <= 1; chain_four <= 0;
            gc_index <= 0; set_reset <= 0; enable_set_reset <= 0; color_compare <= 0; color_dont_care <= 0;
            rotate <= 0; raster_op <= 0; read_map <= 0; write_mode <= 0; shift_mode <= 0;
            memory_map <= 2'd2; read_mode <= 0; gc_odd_even <= 0; graphics <= 0; chain_odd_even <= 0;
            bit_mask <= 0;
            crtc_index <= 0;
            for (i = 0; i < 25; i = i + 1) crtc[i] <= 0;
            attr_index <= 0; attr_pas <= 1; attr_flip <= 0;
            for (i = 0; i < 16; i = i + 1) palette[i] <= 0;
            attr_mode <= 8'h04; overscan <= 0; plane_enable <= 4'hf; pel_pan <= 0; color_select <= 0;
            pel_mask <= 8'hff; dac_read_index <= 0; dac_write_index <= 0;
            dac_read_cycle <= 0; dac_write_cycle <= 0; dac_state <= 2'd1; dac_red <= 0; dac_green <= 0;
            latch <= 0;
        end else begin
            // Intake: a write posts at once; a read parks.
            if (io_write_take) begin io_done <= 1; io_rdata <= 8'hff; io_taken <= 1; end
            if (io_new && !io_write) begin
                read_pending <= 1; read_is_io <= 1; read_port <= io_port; io_taken <= 1;
            end else if (mem_read_take) begin
                read_pending <= 1; read_is_io <= 0; read_address <= mem_address; read_enables <= mem_byteenable;
            end

            if (head_retire) head_loaded <= 0;
            else if (word_accept) begin head_loaded <= 1; remaining <= live & ~group; end

            case (engine)
                E_RUN: begin
                    // Register writes, in FIFO order with the memory writes.
                    if (!fifo_empty && head_io && head_claimed) case (head_port)
                        10'h3c0: begin
                            if (!attr_flip) begin attr_index <= head_byte[4:0]; attr_pas <= head_byte[5]; end
                            else if (attr_index < 5'd16) palette[attr_index[3:0]] <= head_byte;
                            else case (attr_index)
                                5'h10: attr_mode <= {head_byte[7:5], 1'b0, head_byte[3:0]};
                                5'h11: overscan <= head_byte[5:0];
                                5'h12: plane_enable <= head_byte[3:0];
                                5'h13: pel_pan <= head_byte[3:0];
                                5'h14: color_select <= head_byte[3:0];
                                default: ;
                            endcase
                            attr_flip <= !attr_flip;
                        end
                        10'h3c2: misc <= head_byte;
                        10'h3c3: video_enable <= head_byte[0];
                        10'h3c4: seq_index <= head_byte;
                        10'h3c5: case (seq_index)
                            8'd0: begin
                                if (seq_reset[0] && !head_byte[0]) char_map <= 0;
                                seq_reset <= head_byte[1:0];
                            end
                            8'd1: seq_clocking <= head_byte & 8'h3d;
                            8'd2: map_mask <= head_byte[3:0];
                            8'd3: char_map <= head_byte[5:0];
                            8'd4: begin
                                seq_extended <= head_byte[1]; odd_even_disable <= head_byte[2]; chain_four <= head_byte[3];
                            end
                            default: ;
                        endcase
                        10'h3c6: pel_mask <= head_byte;
                        10'h3c7: begin dac_read_index <= head_byte; dac_read_cycle <= 0; dac_state <= 2'd3; end
                        10'h3c8: begin dac_write_index <= head_byte; dac_write_cycle <= 0; dac_state <= 2'd0; end
                        10'h3c9: begin
                            if (dac_write_cycle == 2'd0) dac_red <= head_byte[5:0];
                            if (dac_write_cycle == 2'd1) dac_green <= head_byte[5:0];
                            dac_write_cycle <= dac_write_cycle == 2'd2 ? 2'd0 : dac_write_cycle + 2'd1;
                            if (dac_write_cycle == 2'd2) dac_write_index <= dac_write_index + 8'd1;
                        end
                        10'h3ce: gc_index <= head_byte;
                        10'h3cf: case (gc_index)
                            8'd0: set_reset <= head_byte[3:0];
                            8'd1: enable_set_reset <= head_byte[3:0];
                            8'd2: color_compare <= head_byte[3:0];
                            8'd3: begin rotate <= head_byte[2:0]; raster_op <= head_byte[4:3]; end
                            8'd4: read_map <= head_byte[1:0];
                            8'd5: begin
                                write_mode <= head_byte[1:0]; read_mode <= head_byte[3];
                                gc_odd_even <= head_byte[4]; shift_mode <= head_byte[6:5];
                            end
                            8'd6: begin
                                graphics <= head_byte[0]; chain_odd_even <= head_byte[1]; memory_map <= head_byte[3:2];
                            end
                            8'd7: color_dont_care <= head_byte[3:0];
                            8'd8: bit_mask <= head_byte;
                            default: ;
                        endcase
                        10'h3b4, 10'h3d4: crtc_index <= head_byte[5:0];
                        10'h3b5, 10'h3d5:
                            if (crtc_index <= 6'd24) begin
                                if (!crtc_protected) crtc[crtc_index[4:0]] <= head_byte;
                                else if (crtc_index == 6'd7) crtc[7] <= {crtc[7][7:5], head_byte[4], crtc[7][3:0]};
                            end
                        10'h3ba, 10'h3da: feature <= head_byte & 8'h08;
                        default: ;
                    endcase
                    if (read_pending && fifo_empty) begin
                        if (read_is_io) engine <= E_IO_READ;
                        else begin engine <= E_READ_DRAIN; flush_all <= 1; end
                    end
                end
                E_IO_READ: begin
                    io_rdata <= register_value;
                    io_done <= 1;
                    read_pending <= 0;
                    engine <= E_RUN;
                    if (claimed(read_port[9:0], mono) && video_enable) begin
                        if (read_port[9:0] == 10'h3ba || read_port[9:0] == 10'h3da) attr_flip <= 0;
                        if (read_port[9:0] == 10'h3c9 && dac_state == 2'd3) begin
                            dac_read_cycle <= dac_read_cycle == 2'd2 ? 2'd0 : dac_read_cycle + 2'd1;
                            if (dac_read_cycle == 2'd2) dac_read_index <= dac_read_index + 8'd1;
                        end
                    end
                end
                E_READ_DRAIN: if (coalescer_empty) begin
                    flush_all <= 0;
                    read_received <= 0;
                    // Nothing inside the window, or the VGA disabled: FFh, no latch load.
                    if (!video_enable || (read_enables & read_inside) == 0) begin
                        mem_rdata <= 32'hffffffff;
                        mem_rvalid <= 1;
                        read_pending <= 0;
                        engine <= E_RUN;
                    end else engine <= E_READ_ISSUE;
                end
                E_READ_ISSUE: if (cpu_read_issue) engine <= E_READ_WAIT;
                E_READ_WAIT: if (cpu_read_data) begin
                    read_beats[read_received[1:0]] <= p1_readdata;
                    read_received <= read_received + 3'd1;
                    if (read_received + 3'd1 == read_beat_count) engine <= E_READ_ANSWER;
                end
                E_READ_ANSWER: begin
                    mem_rdata <= read_result;
                    latch <= read_latch;
                    mem_rvalid <= 1;
                    read_pending <= 0;
                    engine <= E_RUN;
                end
                default: engine <= E_RUN;
            endcase
        end
    end

    // ---- Scanout, DAC and capture ---------------------------------------------
    pc386_vga_scanout #(
        .CAPTURE_BASE(CAPTURE_BASE),
        .DOT0_N(DOT0_N), .DOT0_D(DOT0_D), .DOT1_N(DOT1_N), .DOT1_D(DOT1_D)
    ) scanout (
        .clk(clk), .reset(reset),
        .misc(misc), .video_enable(video_enable), .seq_reset(seq_reset), .seq_clocking(seq_clocking),
        .char_map(char_map), .shift_mode(shift_mode), .graphics(graphics),
        .crtc0(crtc[0]), .crtc1(crtc[1]), .crtc6(crtc[6]), .crtc7(crtc[7]), .crtc8(crtc[8]), .crtc9(crtc[9]),
        .crtc10(crtc[10]), .crtc11(crtc[11]), .crtc12(crtc[12]), .crtc13(crtc[13]), .crtc14(crtc[14]),
        .crtc15(crtc[15]), .crtc16(crtc[16]), .crtc17(crtc[17]), .crtc18(crtc[18]), .crtc19(crtc[19]),
        .crtc20(crtc[20]), .crtc23(crtc[23]), .crtc24(crtc[24]),
        .attr_pas(attr_pas), .attr_mode(attr_mode), .overscan(overscan), .plane_enable(plane_enable),
        .pel_pan(pel_pan), .color_select(color_select), .pel_mask(pel_mask),
        .palette0(palette[0]), .palette1(palette[1]), .palette2(palette[2]), .palette3(palette[3]),
        .palette4(palette[4]), .palette5(palette[5]), .palette6(palette[6]), .palette7(palette[7]),
        .palette8(palette[8]), .palette9(palette[9]), .palette10(palette[10]), .palette11(palette[11]),
        .palette12(palette[12]), .palette13(palette[13]), .palette14(palette[14]), .palette15(palette[15]),
        .dac_write(dac_write), .dac_write_index(dac_write_index),
        .dac_write_data({dac_red, dac_green, head_byte[5:0]}),
        .dac_read_index(dac_read_index), .dac_read_cycle(dac_read_cycle), .dac_read_data(dac_read_data),
        .status(status),
        .fetch_request(scan_request), .fetch_beat(scan_beat), .fetch_grant(scan_grant),
        .fetch_valid(scan_response), .fetch_data(p1_readdata),
        .cap_address(cap_address), .cap_burstcount(cap_burstcount), .cap_write(cap_write),
        .cap_writedata(cap_writedata), .cap_waitrequest(cap_waitrequest),
        .frame_buffer(frame_buffer), .frame_width(frame_width), .frame_height(frame_height),
        .frame_sequence(frame_sequence), .present_buffer(present_buffer),
        .frame_start(frame_start), .frame_done(frame_done), .start_latched(start_latched),
        .frame_count(frame_count), .underflows(scanout_underflows), .capture_drops(capture_drops)
    );
    wire unused = ^{owner_full, owner_empty, owner_fill, fifo_count, gc_odd_even, chain_odd_even, seq_extended, crtc_index,
                    crtc[2], crtc[3], crtc[4], crtc[5], crtc[21], crtc[22]};
endmodule
