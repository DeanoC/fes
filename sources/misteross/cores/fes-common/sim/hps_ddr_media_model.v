// SPDX-License-Identifier: GPL-2.0-or-later
// Behavioral cyclonev_hps_interface_fpga2sdram for a shell whose session
// display reads port 0 and whose media store uses port 1 (fes_media_ddr).
// Port 0 answers as cores/fes-menu/sim/ddr_model.v does, with synthetic
// launcher pixels from the two menu slots. Port 1 is MEDIA_WORDS 64-bit words
// of memory from MEDIA_BASE, written a beat at a time with byte enables and
// read in bursts. Each port runs on its own command clock. Port 1 refuses
// commands at random and while a read burst is owed, and returns read data
// after a delay with gaps. violation_plane and violation_media flag a
// command outside the simulated spans, a malformed command, a multi-beat
// write, write data that does not match a presented write command, or any
// use of ports 2 to 5.
/* verilator lint_off UNUSEDSIGNAL */
module cyclonev_hps_interface_fpga2sdram #(
    parameter [31:0] MEDIA_BASE = 32'h31000000,
    parameter integer MEDIA_WORDS = 65536
) (
    input wire [5:0] cfg_axi_mm_select,
    input wire [17:0] cfg_cport_rfifo_map,
    input wire [11:0] cfg_cport_type,
    input wire [17:0] cfg_cport_wfifo_map,
    input wire [11:0] cfg_port_width,
    input wire [15:0] cfg_rfifo_cport_map,
    input wire [15:0] cfg_wfifo_cport_map,
    input wire cmd_port_clk_0, input wire cmd_port_clk_1, input wire cmd_port_clk_2,
    input wire cmd_port_clk_3, input wire cmd_port_clk_4, input wire cmd_port_clk_5,
    input wire cmd_valid_0, input wire cmd_valid_1, input wire cmd_valid_2,
    input wire cmd_valid_3, input wire cmd_valid_4, input wire cmd_valid_5,
    input wire [59:0] cmd_data_0, input wire [59:0] cmd_data_1, input wire [59:0] cmd_data_2,
    input wire [59:0] cmd_data_3, input wire [59:0] cmd_data_4, input wire [59:0] cmd_data_5,
    output wire cmd_ready_0, output wire cmd_ready_1, output wire cmd_ready_2,
    output wire cmd_ready_3, output wire cmd_ready_4, output wire cmd_ready_5,
    input wire wrack_ready_0, input wire wrack_ready_1, input wire wrack_ready_2,
    input wire wrack_ready_3, input wire wrack_ready_4, input wire wrack_ready_5,
    output wire [9:0] wrack_data_0, output wire [9:0] wrack_data_1, output wire [9:0] wrack_data_2,
    output wire [9:0] wrack_data_3, output wire [9:0] wrack_data_4, output wire [9:0] wrack_data_5,
    output wire wrack_valid_0, output wire wrack_valid_1, output wire wrack_valid_2,
    output wire wrack_valid_3, output wire wrack_valid_4, output wire wrack_valid_5,
    input wire wr_clk_0, input wire wr_clk_1, input wire wr_clk_2, input wire wr_clk_3,
    input wire wr_valid_0, input wire wr_valid_1, input wire wr_valid_2, input wire wr_valid_3,
    input wire [89:0] wr_data_0, input wire [89:0] wr_data_1,
    input wire [89:0] wr_data_2, input wire [89:0] wr_data_3,
    output wire wr_ready_0, output wire wr_ready_1, output wire wr_ready_2, output wire wr_ready_3,
    input wire rd_clk_0, input wire rd_clk_1, input wire rd_clk_2, input wire rd_clk_3,
    input wire rd_ready_0, input wire rd_ready_1, input wire rd_ready_2, input wire rd_ready_3,
    output reg [79:0] rd_data_0, output reg [79:0] rd_data_1,
    output reg [79:0] rd_data_2, output reg [79:0] rd_data_3,
    output reg rd_valid_0, output reg rd_valid_1, output reg rd_valid_2, output reg rd_valid_3
);
    localparam integer MW = $clog2(MEDIA_WORDS);

    reg violation_plane /* verilator public_flat_rd */ = 1'b0;
    reg violation_media /* verilator public_flat_rd */ = 1'b0;
    reg [7:0] alpha /* verilator public_flat_rw */ = 8'd0;
    reg [63:0] media [0:MEDIA_WORDS-1] /* verilator public_flat_rw */;
    reg [31:0] media_writes /* verilator public_flat_rd */ = 32'd0;
    reg [31:0] media_reads /* verilator public_flat_rd */ = 32'd0;

    // ---------------------------------------------------- port 0: the plane
    reg [7:0] remaining0 = 8'd0;
    reg [31:0] next_byte = 32'd0;
    assign cmd_ready_0 = remaining0 == 8'd0;
    function [31:0] pixel;
        input [31:0] a;
        reg [31:0] index;
        begin
            index=(a-32'h30000000-(a[22] ? 32'h00400000 : 32'd0))>>2;
            pixel={alpha,index[7:0],index[15:8],a[22] ? 8'h77 : 8'h22};
        end
    endfunction
    initial begin
        rd_valid_0 = 0; rd_valid_1 = 0; rd_valid_2 = 0; rd_valid_3 = 0;
        rd_data_0 = 0; rd_data_1 = 0; rd_data_2 = 0; rd_data_3 = 0;
    end
    always @(posedge cmd_port_clk_0) begin
        rd_valid_0 <= 0; rd_valid_1 <= 0;
        if ({cmd_valid_5, cmd_valid_4, cmd_valid_3, cmd_valid_2} != 4'd0 ||
            {wr_valid_3, wr_valid_1, wr_valid_0} != 3'd0)
            violation_plane <= 1;
        if (cmd_valid_0 && cmd_ready_0) begin
            if (cmd_data_0[1:0] != 2'b01 || cmd_data_0[59:42] != 18'd0 ||
                cmd_data_0[33:30] != 4'd0 || cmd_data_0[41:34] == 8'd0 ||
                cmd_data_0[41:34] > 8'd128 ||
                {cmd_data_0[29:2], 4'd0} < 32'h30000000 ||
                {cmd_data_0[29:2], 4'd0} + {20'd0, cmd_data_0[41:34], 4'd0} > 32'h30784000 ||
                ({cmd_data_0[29:2], 4'd0} >= 32'h30384000 && {cmd_data_0[29:2], 4'd0} < 32'h30400000))
                violation_plane <= 1;
            remaining0 <= cmd_data_0[41:34];
            next_byte <= {cmd_data_0[29:2], 4'd0};
        end
        if (remaining0 != 8'd0) begin
            rd_data_0 <= {16'd0, pixel(next_byte + 32'd4), pixel(next_byte)};
            rd_data_1 <= {16'd0, pixel(next_byte + 32'd12), pixel(next_byte + 32'd8)};
            rd_valid_0 <= 1; rd_valid_1 <= 1;
            next_byte <= next_byte + 32'd16;
            remaining0 <= remaining0 - 8'd1;
        end
    end

    // --------------------------------------------------- port 1: the media
    reg [15:0] lfsr = 16'hace1;
    wire refuse = lfsr[3:0] == 4'd0 || lfsr[7:4] == 4'd5;
    reg [7:0] remaining1 = 8'd0;   // read beats owed
    reg [3:0] delay1 = 4'd0;       // clocks before the first beat
    reg [MW-1:0] read_word = 0;
    wire [31:0] media_address = {cmd_data_1[30:2], 3'd0};
    wire [31:0] media_bytes = {21'd0, cmd_data_1[41:34], 3'd0};
    assign cmd_ready_1 = remaining1 == 8'd0 && !refuse;
    always @(posedge cmd_port_clk_1) begin
        lfsr <= {lfsr[14:0], lfsr[15] ^ lfsr[13] ^ lfsr[12] ^ lfsr[10]};
        rd_valid_2 <= 0;
        // fes_hps_ddr raises write data with every presented write command.
        if (wr_valid_2 != (cmd_valid_1 && cmd_data_1[1]))
            violation_media <= 1;
        if (cmd_valid_1 && cmd_ready_1) begin
            if (cmd_data_1[1:0] == 2'b00 || cmd_data_1[1:0] == 2'b11 ||
                cmd_data_1[59:42] != 18'd0 || cmd_data_1[33:31] != 3'd0 ||
                cmd_data_1[41:34] == 8'd0 || cmd_data_1[41:34] > 8'd128 ||
                media_address < MEDIA_BASE ||
                media_address + media_bytes > MEDIA_BASE + MEDIA_WORDS * 8 ||
                (cmd_data_1[1] && (cmd_data_1[41:34] != 8'd1 || !wr_valid_2)))
                violation_media <= 1;
            else if (cmd_data_1[1]) begin
                for (integer lane = 0; lane < 8; lane = lane + 1)
                    if (wr_data_2[80 + lane])
                        media[MW'((media_address - MEDIA_BASE) >> 3)][lane * 8 +: 8] <=
                            wr_data_2[lane * 8 +: 8];
                media_writes <= media_writes + 32'd1;
            end else begin
                remaining1 <= cmd_data_1[41:34];
                delay1 <= 4'd4 + {2'd0, lfsr[9:8]};
                read_word <= MW'((media_address - MEDIA_BASE) >> 3);
                media_reads <= media_reads + 32'd1;
            end
        end
        if (remaining1 != 8'd0) begin
            if (delay1 != 4'd0) delay1 <= delay1 - 4'd1;
            else if (!lfsr[2] || !lfsr[11]) begin
                rd_data_2 <= {16'd0, media[read_word]};
                rd_valid_2 <= 1;
                read_word <= read_word + 1'b1;
                remaining1 <= remaining1 - 8'd1;
            end
        end
    end

    assign {cmd_ready_2, cmd_ready_3, cmd_ready_4, cmd_ready_5} = 4'd0;
    assign {wr_ready_0, wr_ready_1, wr_ready_2, wr_ready_3} = 4'd0;
    assign {wrack_valid_0, wrack_valid_1, wrack_valid_2, wrack_valid_3, wrack_valid_4, wrack_valid_5} = 6'd0;
    assign {wrack_data_0, wrack_data_1, wrack_data_2, wrack_data_3, wrack_data_4, wrack_data_5} = 60'd0;
endmodule
