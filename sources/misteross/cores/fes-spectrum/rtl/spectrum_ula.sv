// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum ULA timing: T-state clock, frame interrupt, CPU contention,
// screen fetch, floating bus and the rendered picture.
//
// tick_p/tick_n are the free-running T-state edges. The CPU receives them as
// ce_p/ce_n unless the ULA holds its clock high: at tick_n of a contended T
// state, a contention request suppresses ce_n and the following ce_p, exactly
// as the real ULA stretches the high half of the CPU clock.
//
// Each model's frame, interrupt and first paper pixel follow Fuse's
// libspectrum timing table. hx/vy count T states within a line and lines
// within a frame such that the first paper pixel is at hx=32, vy=0. A paper
// chunk's bitmap and attribute are read two T states after the chunk begins,
// so a write whose T1 began in the chunk's first T state is shown and a later
// one is not; a chunk takes the border register as it was at its last T
// state, so an OUT whose T2 began by then is shown. This is Fuse's display
// model. The visible picture is 320x240: 32 border pixels left and right,
// 24 lines above and below the 256x192 paper.
module spectrum_ula #(
    parameter integer MODEL = 0  // 0 48K, 1 128K/+2, 2 +2A/+3, 3 Pentagon 128
) (
    input  wire        clk,
    input  wire        reset,
    // Per-clock T-state speed: the rational rate is PHASE_ADD/PHASE_MOD of clk.
    output reg         tick_p = 1'b0,
    output reg         tick_n = 1'b0,
    // Contention: the CPU's presented cycle asks to be stretched in this slot.
    input  wire        contend_request,
    output wire        cpu_ce_p,
    output wire        cpu_ce_n,
    output wire        int_n,
    // Screen memory read port: RAM bank and 14-bit offset. screen7 is the
    // 128K-class shadow-screen select (port $7FFD bit 3).
    input  wire        screen7,
    output reg  [2:0]  screen_bank = 3'd5,
    output reg  [13:0] screen_addr = 14'd0,
    input  wire [7:0]  screen_data,
    input  wire [2:0]  border,
    output reg  [7:0]  floating = 8'hff,
    // 8 pixels of 4-bit {bright, green, red, blue}, leftmost in [31:28].
    output reg         pixel_we = 1'b0,
    output reg  [7:0]  pixel_row = 8'd0,
    output reg  [5:0]  pixel_chunk = 6'd0,
    output reg  [31:0] pixel_data = 32'd0,
    output reg         frame_done = 1'b0
);
    localparam integer T_LINE   = (MODEL == 1 || MODEL == 2) ? 228 : 224;
    localparam integer LINES    = MODEL == 3 ? 320 : (MODEL == 0 ? 312 : 311);
    localparam integer FRAME    = T_LINE * LINES;
    localparam integer INT_LEN  = (MODEL == 1 || MODEL == 3) ? 36 : 32;
    localparam integer TOP_LEFT = MODEL == 0 ? 14336 : MODEL == 1 ? 14362 :
                                  MODEL == 2 ? 14365 : 17988;
    // 0 none, 1 Sinclair 6,5,4,3,2,1,0,0 from TOP_LEFT-1, 2 Amstrad
    // 1,0,7,6,5,4,3,2 from TOP_LEFT-4.
    localparam integer CONTENTION = MODEL == 3 ? 0 : MODEL == 2 ? 2 : 1;
    // Floating bus: 1 Sinclair ULA, 2 Amstrad (one T state later, bit 0
    // set), 3 Pentagon port $FF (the Sinclair pattern four T states later,
    // from its 36 T left border in Fuse's model).
    localparam integer FLOATING = MODEL == 2 ? 2 : MODEL == 3 ? 3 : 1;
    localparam bit SHADOW = MODEL != 0;
    // 48K and Pentagon run 3.5 MHz; 128K/+2A 3.5469 MHz, from 52.224 MHz.
    localparam integer PHASE_ADD = (MODEL == 1 || MODEL == 2) ? 11823 : 875;
    localparam integer PHASE_MOD = (MODEL == 1 || MODEL == 2) ? 174080 : 13056;
    // Counters at frame T state 0: hx = (32 - TOP_LEFT) mod T_LINE.
    localparam integer LINE_BACK = (TOP_LEFT - 32 + T_LINE - 1) / T_LINE;
    localparam integer HX0 = LINE_BACK * T_LINE + 32 - TOP_LEFT;
    localparam integer VY0 = LINES - LINE_BACK;

    // ---------------------------------------------------------------- clock
    reg [17:0] phase = 18'd0;
    reg [3:0] since_p = 4'd0;
    always @(posedge clk) begin
        tick_p <= 1'b0;
        tick_n <= 1'b0;
        if (reset) begin
            phase <= 18'd0;
            since_p <= 4'd0;
        end else if ({1'b0, phase} + PHASE_ADD >= PHASE_MOD) begin
            phase <= phase + 18'(PHASE_ADD) - 18'(PHASE_MOD);
            since_p <= 4'd0;
            tick_p <= 1'b1;
        end else begin
            phase <= phase + 18'(PHASE_ADD);
            if (since_p == 4'd6) tick_n <= 1'b1;
            if (since_p != 4'd15) since_p <= since_p + 4'd1;
        end
    end

    // --------------------------------------------------------- frame timing
    reg [16:0] t = 17'd0;
    reg [8:0] hx = 9'(HX0);
    reg [8:0] vy = 9'(VY0);
    reg [3:0] flash_count = 4'd0;
    wire flash = flash_count[3];
    always @(posedge clk) begin
        if (reset) begin
            t <= 17'd0;
            hx <= 9'(HX0);
            vy <= 9'(VY0);
            flash_count <= 4'd0;
        end else if (tick_p) begin
            if (t == 17'(FRAME - 1)) begin
                t <= 17'd0;
                hx <= 9'(HX0);
                vy <= 9'(VY0);
                flash_count <= flash_count + 4'd1;
            end else begin
                t <= t + 17'd1;
                if (hx == 9'(T_LINE - 1)) begin
                    hx <= 9'd0;
                    vy <= vy == 9'(LINES - 1) ? 9'd0 : vy + 9'd1;
                end else begin
                    hx <= hx + 9'd1;
                end
            end
        end
    end
    // The CPU samples INT at the ce_p that starts an instruction's last T
    // state, when t still names the slot before it. Asserting INT two slots
    // early accepts it exactly at instruction boundaries 0..INT_LEN-1, as
    // the hardware does and Fuse models.
    assign int_n = !(t >= 17'(FRAME - 2) || t < 17'(INT_LEN - 2));

    // ----------------------------------------------------------- contention
    // A stretch is decided when the CPU first asks in a slot, from the slot's
    // place in the pattern, and then lasts its whole length even past the
    // end of the window (the +2A's last group), as Fuse charges it.
    wire paper_line = vy < 9'd192;
    wire [8:0] sinclair_x = hx - 9'd31;
    wire [8:0] amstrad_x = hx - 9'd28;
    reg [2:0] delay_now;
    always @* begin
        delay_now = 3'd0;
        if (CONTENTION == 1 && paper_line && hx >= 9'd31 && hx < 9'd159)
            delay_now = sinclair_x[2:0] < 3'd6 ? 3'd6 - sinclair_x[2:0] : 3'd0;
        else if (CONTENTION == 2 && paper_line && hx >= 9'd28 && hx < 9'd156)
            delay_now = amstrad_x[2:0] == 3'd0 ? 3'd1 : amstrad_x[2:0] == 3'd1 ? 3'd0 :
                        3'd9 - amstrad_x[2:0];
    end
    // The CPU's presented cycle and the slot position are stable from ce_p to
    // the middle of the T state, so both are registered a clock ahead of
    // tick_n; ce_n then depends only on registers.
    reg request_q = 1'b0;
    reg [2:0] delay_q = 3'd0;
    always @(posedge clk) begin
        request_q <= contend_request;
        delay_q <= delay_now;
    end
    reg [2:0] stall = 3'd0;
    wire start_hold = stall == 3'd0 && request_q && delay_q != 3'd0;
    wire hold_now = stall != 3'd0 || start_hold;
    reg held = 1'b0;
    always @(posedge clk) begin
        if (reset) begin
            held <= 1'b0;
            stall <= 3'd0;
        end else if (tick_n) begin
            held <= hold_now;
            if (stall != 3'd0) stall <= stall - 3'd1;
            else if (start_hold) stall <= delay_q - 3'd1;
        end
    end
    assign cpu_ce_p = tick_p && !held;
    assign cpu_ce_n = tick_n && !hold_now;

    // ------------------------------------------------- screen and floating bus
    function [13:0] bitmap_offset(input [7:0] y, input [4:0] column);
        bitmap_offset = {1'b0, y[7:6], y[2:0], y[5:3], column};
    endfunction
    function [13:0] attr_offset(input [7:0] y, input [4:0] column);
        attr_offset = {4'b0110, y[7:3], column};
    endfunction

    // Floating bus position: d is the T state relative to the paper start.
    wire [8:0] d = hx - 9'd32;
    wire [8:0] d_late = hx - 9'd33;   // Amstrad: one T state later
    wire in_paper_t = paper_line && hx >= 9'd32 && hx < 9'd160;
    wire [2:0] float_phase = FLOATING == 2 ? d_late[2:0] :
                             FLOATING == 3 ? d[2:0] + 3'd4 : d[2:0];
    wire float_live = in_paper_t && float_phase >= 3'd2 && float_phase <= 3'd5;
    wire [4:0] float_column = {d[6:3], 1'b0} + {4'd0, float_phase >= 3'd4};
    wire float_attr = float_phase[0];

    // A chunk's bitmap and attribute are read at the start of the T state
    // two after the chunk begins (hx = 34 + 4c), so a write whose T1 began
    // in the chunk's first T state is seen and a later one is not. 128K-class
    // models read both screens then; the bank is chosen when the chunk ends,
    // so an OUT to $7FFD with T2 up to its last T state switches it.
    // Screen port schedule, one read per clock from the clock after tick_p:
    //   0 floating-bus byte, 1/2 bank 5 bitmap/attribute, 3/4 bank 7.
    reg tick_p_d = 1'b0;
    always @(posedge clk) tick_p_d <= tick_p;
    wire [8:0] fetch_x = hx - 9'd34;
    wire fetch_now = paper_line && hx >= 9'd34 && hx < 9'd162 && fetch_x[1:0] == 2'd0;
    localparam [2:0] S_IDLE = 3'd0, S_FLOAT = 3'd1, S_B5 = 3'd2, S_A5 = 3'd3,
                     S_B7 = 3'd4, S_A7 = 3'd5;
    reg [2:0] step = S_IDLE;
    reg [2:0] issued_d1 = S_IDLE, issued_d2 = S_IDLE, issued_d3 = S_IDLE;
    reg [4:0] fetch_column = 5'd0;
    reg [7:0] fetch_y = 8'd0;
    reg [7:0] bitmap5 = 8'd0, attr5 = 8'd0, bitmap7 = 8'd0, attr7 = 8'd0;

    always @(posedge clk) begin
        issued_d1 <= S_IDLE;
        issued_d2 <= issued_d1;
        issued_d3 <= issued_d2;
        if (reset) begin
            step <= S_IDLE;
            floating <= 8'hff;
        end else begin
            if (tick_p_d) begin
                // Counters for the new slot are visible the clock after tick_p.
                fetch_column <= fetch_x[6:2];
                fetch_y <= vy[7:0];
                if (float_live) begin
                    screen_bank <= SHADOW && screen7 ? 3'd7 : 3'd5;
                    screen_addr <= float_attr ? attr_offset(vy[7:0], float_column)
                                              : bitmap_offset(vy[7:0], float_column);
                    issued_d1 <= S_FLOAT;
                end else floating <= 8'hff;
                step <= fetch_now ? S_B5 : S_IDLE;
            end else if (step != S_IDLE) begin
                screen_bank <= step >= S_B7 ? 3'd7 : 3'd5;
                screen_addr <= step[0] ? attr_offset(fetch_y, fetch_column)
                                       : bitmap_offset(fetch_y, fetch_column);
                issued_d1 <= step;
                step <= step == S_A7 || (!SHADOW && step == S_A5) ? S_IDLE : step + 3'd1;
            end
            // The address register, block read and bank select take three
            // clocks; capture on the fourth edge.
            case (issued_d3)
                S_FLOAT: floating <= FLOATING == 2 ? (screen_data | 8'h01) : screen_data;
                S_B5: bitmap5 <= screen_data;
                S_A5: attr5 <= screen_data;
                S_B7: bitmap7 <= screen_data;
                S_A7: attr7 <= screen_data;
                default: ;
            endcase
        end
    end

    // --------------------------------------------------------------- picture
    // Chunk k (0..39) of a visible line ends at hx = 20 + 4k; paper chunks are
    // k = 4..35. The border colour is the value before this tick_p.
    wire visible_line = vy < 9'd216 || vy >= 9'(LINES - 24);
    wire [8:0] chunk_x = hx - 9'd20;
    wire chunk_end = visible_line && hx >= 9'd20 && hx <= 9'd176 && chunk_x[1:0] == 2'd0;
    wire [5:0] chunk = chunk_x[7:2];
    wire paper_chunk = paper_line && chunk >= 6'd4 && chunk < 6'd36;
    wire [7:0] row = vy >= 9'(LINES - 24) ? 8'(vy - 9'(LINES - 24)) : 8'(vy + 9'd24);
    reg [2:0] border_q = 3'd7;
    reg screen7_q = 1'b0;

    function [31:0] paper_pixels(input [7:0] bits, input [7:0] attr, input flash_on);
        integer i;
        reg swap;
        reg [3:0] ink, paper;
        begin
            swap = attr[7] && flash_on;
            ink = {attr[6], swap ? attr[5:3] : attr[2:0]};
            paper = {attr[6], swap ? attr[2:0] : attr[5:3]};
            for (i = 0; i < 8; i = i + 1)
                paper_pixels[31 - 4*i -: 4] = bits[7 - i] ? ink : paper;
        end
    endfunction

    always @(posedge clk) begin
        pixel_we <= 1'b0;
        frame_done <= 1'b0;
        // The border register is sampled with the T-state edge.
        if (tick_p) begin
            border_q <= border;
            screen7_q <= SHADOW && screen7;
        end
        if (tick_p_d && chunk_end) begin
            pixel_we <= 1'b1;
            pixel_row <= row;
            pixel_chunk <= chunk;
            pixel_data <= paper_chunk ? paper_pixels(screen7_q ? bitmap7 : bitmap5,
                                                     screen7_q ? attr7 : attr5, flash)
                                      : {8{1'b0, border_q}};
            frame_done <= row == 8'd239 && chunk == 6'd39;
        end
    end
endmodule
