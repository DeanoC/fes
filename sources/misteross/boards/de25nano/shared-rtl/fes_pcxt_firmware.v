// SPDX-License-Identifier: GPL-2.0-or-later
// The PC/XT firmware image, filled at launch (fes.fabric.firmware-upload),
// for a board without linked ROM lanes. The image is the DE10 package's two
// firmware objects in order: the 64 KiB system ROM window (F0000-FFFFF, the
// BIOS top-aligned and FF below it) and the 4 KiB character ROM (256
// characters of 16 rows). On the DE10 the bitstream holds both: pcxt_boot
// copies the window into SDRAM once after a cold start and the font is a
// block ROM. Here the window goes through the same pcxt_system firmware port
// pcxt_boot drives, one byte at a time in address order, and the font into
// a writable copy with the DE10 font's two combinational read ports.
//
// pcxt_system admits exactly one sequential window after each cold reset.
// The front end restarts an image at byte 0 on a new Begin; a pair at byte 0
// after any byte of an earlier image has been stored therefore starts a
// fresh power-on: `restart` asserts a cold reset of the machine for a few
// clocks before that pair is stored. Each uploaded pair is acknowledged once
// both its bytes are stored. No FPGA-family primitive belongs here.
module fes_pcxt_firmware (
    input  wire        clk,
    // The machine's cold reset, including this module's restart.
    input  wire        cold_reset,
    output reg         restart = 1'b0,
    input  wire [16:0] write_addr,
    input  wire [15:0] write_data,
    input  wire [1:0]  write_enable,
    output reg         write_ready = 1'b0,
    // pcxt_system's byte loader (as pcxt_boot drives it).
    output reg         firmware_valid = 1'b0,
    output reg  [15:0] firmware_address = 16'd0,
    output reg  [7:0]  firmware_data = 8'd0,
    input  wire        firmware_ready, firmware_done,
    input  wire [11:0] cga_font_address, mda_font_address,
    output wire [7:0]  cga_font_data, mda_font_data
);
    localparam [2:0] IDLE = 3'd0, RESTART = 3'd1, OFFER = 3'd2, WAIT = 3'd3, ACK = 3'd4;
    reg [2:0] state = IDLE;
    reg [1:0] restart_count = 2'd0;
    reg started = 1'b0, high = 1'b0;
    // The DE10 font is a ROM read combinationally by both display cards. Each
    // card reads its own copy, a memory with one write and one asynchronous
    // read port, which the device builds from MLABs. A pair is one 16-bit
    // word: both bytes are written together.
    wire font = write_addr[16];
    wire font_write = state == IDLE && write_enable[0] && font;
    wire [15:0] glyph_pair = {write_enable[1] ? write_data[15:8] : 8'hff, write_data[7:0]};
    (* ramstyle = "MLAB, no_rw_check" *) reg [15:0] cga_glyphs [0:2047];
    (* ramstyle = "MLAB, no_rw_check" *) reg [15:0] mda_glyphs [0:2047];
    always @(posedge clk) if (font_write) cga_glyphs[write_addr[11:1]] <= glyph_pair;
    always @(posedge clk) if (font_write) mda_glyphs[write_addr[11:1]] <= glyph_pair;
    // The cards' addresses come straight from their VRAM read registers.
    // Kept nodes stop synthesis absorbing those registers into the MLABs as
    // a registered read address, which leaves the VRAM read asynchronous and
    // builds a VRAM copy from registers.
    (* keep *) wire [11:0] cga_address = cga_font_address;
    (* keep *) wire [11:0] mda_address = mda_font_address;
    wire [15:0] cga_pair = cga_glyphs[cga_address[11:1]];
    wire [15:0] mda_pair = mda_glyphs[mda_address[11:1]];
    assign cga_font_data = cga_address[0] ? cga_pair[15:8] : cga_pair[7:0];
    assign mda_font_data = mda_address[0] ? mda_pair[15:8] : mda_pair[7:0];
    always @(posedge clk) begin
        write_ready <= 1'b0;
        case (state)
            IDLE: if (write_enable[0] && !write_ready) begin
                if (write_addr == 17'd0 && started) begin
                    state <= RESTART;
                    restart <= 1'b1;
                    restart_count <= 2'd0;
                end else if (font) begin
                    write_ready <= 1'b1;
                    state <= ACK;
                end else begin
                    started <= 1'b1;
                    high <= 1'b0;
                    firmware_address <= write_addr[15:0];
                    firmware_data <= write_data[7:0];
                    firmware_valid <= 1'b1;
                    state <= OFFER;
                end
            end
            RESTART: begin
                restart_count <= restart_count + 2'd1;
                if (&restart_count) begin
                    restart <= 1'b0;
                    started <= 1'b0;
                    state <= IDLE;
                end
            end
            OFFER: if (firmware_ready) begin
                firmware_valid <= 1'b0;
                state <= WAIT;
            end
            WAIT: if (firmware_done) begin
                if (!high && write_enable[1]) begin
                    high <= 1'b1;
                    firmware_address <= firmware_address + 16'd1;
                    firmware_data <= write_data[15:8];
                    firmware_valid <= 1'b1;
                    state <= OFFER;
                end else begin
                    write_ready <= 1'b1;
                    state <= ACK;
                end
            end
            // The front end acknowledges on write_ready; its enable falls on
            // the next clock.
            default: state <= IDLE;
        endcase
        // A cold reset from the board abandons a pair in flight; the front end
        // holds it and it is offered again from the start.
        if (cold_reset && !restart) begin
            firmware_valid <= 1'b0;
            started <= 1'b0;
            if (state != RESTART) state <= IDLE;
        end
    end
endmodule
