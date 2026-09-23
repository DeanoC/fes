// SPDX-License-Identifier: GPL-2.0-or-later
// 50 MHz 16-bit SDR SDRAM, burst length 1, CAS latency 2.
// 128 MB is 64M halfwords: 11 column bits, 2 banks, 13 row bits.
// Column A10 is a real column bit. Precharge is explicit, not A10.
// A refresh is inserted between commands so a full-chip scan keeps its data.
module sdram_addon_port (
    input wire clk,
    input wire start,
    input wire write,
    input wire [25:0] addr,
    input wire [15:0] wdata,
    output reg done,
    output reg [15:0] rdata,
    output wire sdram_clk,
    output reg sdram_cke,
    output reg sdram_ncs,
    output reg sdram_nras,
    output reg sdram_ncas,
    output reg sdram_nwe,
    output reg [1:0] sdram_ba,
    output reg [12:0] sdram_a,
    output reg sdram_dqml,
    output reg sdram_dqmh,
    output reg [15:0] dq_out,
    output reg dq_oe,
    input wire [15:0] dq_in
);
    localparam [3:0] ST_BOOT = 4'd0;
    localparam [3:0] ST_PRE = 4'd1;
    localparam [3:0] ST_REF1 = 4'd2;
    localparam [3:0] ST_REF2 = 4'd3;
    localparam [3:0] ST_MRS = 4'd4;
    localparam [3:0] ST_IDLE = 4'd5;
    localparam [3:0] ST_REF = 4'd6;
    localparam [3:0] ST_REFW = 4'd7;
    localparam [3:0] ST_ROW = 4'd8;
    localparam [3:0] ST_ACT = 4'd9;
    localparam [3:0] ST_RW = 4'd10;
    localparam [3:0] ST_HOLD = 4'd11;
    localparam [3:0] ST_CAP = 4'd12;
    localparam [3:0] ST_PRE2 = 4'd13;
    localparam [3:0] ST_FINISH = 4'd14;

    reg [3:0] state = ST_BOOT;
    reg [12:0] wait_count = 13'd0;
    reg [8:0] refresh_div = 9'd0;
    reg refresh_due = 1'b0;
    reg seen = 1'b0;
    reg writing = 1'b0;
    reg [25:0] held_addr = 26'd0;
    reg [15:0] held_data = 16'h0000;

    assign sdram_clk = clk;

    always @(posedge clk) begin
        done <= 1'b0;
        dq_oe <= 1'b0;
        sdram_dqml <= 1'b0;
        sdram_dqmh <= 1'b0;
        sdram_ncs <= 1'b0;
        sdram_nras <= 1'b1;
        sdram_ncas <= 1'b1;
        sdram_nwe <= 1'b1;
        if (refresh_div == 9'd390) begin
            refresh_div <= 9'd0;
            refresh_due <= 1'b1;
        end else begin
            refresh_div <= refresh_div + 9'd1;
        end
        case (state)
            ST_BOOT: begin
                sdram_cke <= 1'b0;
                sdram_ncs <= 1'b1;
                if (wait_count == 13'd5000) begin
                    sdram_cke <= 1'b1;
                    state <= ST_PRE;
                    wait_count <= 13'd0;
                end else begin
                    wait_count <= wait_count + 13'd1;
                end
            end
            ST_PRE, ST_PRE2: begin
                sdram_cke <= 1'b1;
                sdram_nras <= 1'b0;
                sdram_nwe <= 1'b0;
                sdram_a[10] <= 1'b1;
                state <= (state == ST_PRE) ? ST_REF1 : ST_FINISH;
            end
            ST_REF1, ST_REF2: begin
                sdram_cke <= 1'b1;
                sdram_nras <= 1'b0;
                sdram_ncas <= 1'b0;
                state <= (state == ST_REF1) ? ST_REF2 : ST_MRS;
            end
            ST_MRS: begin
                sdram_cke <= 1'b1;
                sdram_nras <= 1'b0;
                sdram_ncas <= 1'b0;
                sdram_nwe <= 1'b0;
                sdram_ba <= 2'b00;
                sdram_a <= 13'h0020;
                state <= ST_IDLE;
            end
            ST_IDLE: begin
                sdram_cke <= 1'b1;
                sdram_ncs <= 1'b1;
                if (!start)
                    seen <= 1'b0;
                else if (!seen) begin
                    seen <= 1'b1;
                    writing <= write;
                    held_addr <= addr;
                    held_data <= wdata;
                    if (refresh_due) begin
                        state <= ST_REF;
                    end else begin
                        sdram_ba <= addr[12:11];
                        sdram_a <= addr[25:13];
                        sdram_ncs <= 1'b0;
                        sdram_nras <= 1'b0;
                        state <= ST_ACT;
                    end
                end
            end
            ST_REF: begin
                sdram_cke <= 1'b1;
                sdram_nras <= 1'b0;
                sdram_ncas <= 1'b0;
                refresh_due <= 1'b0;
                wait_count <= 13'd0;
                state <= ST_REFW;
            end
            ST_REFW: begin
                sdram_cke <= 1'b1;
                if (wait_count == 13'd4) begin
                    state <= ST_ROW;
                end else begin
                    wait_count <= wait_count + 13'd1;
                end
            end
            ST_ROW: begin
                sdram_cke <= 1'b1;
                sdram_ba <= held_addr[12:11];
                sdram_a <= held_addr[25:13];
                sdram_nras <= 1'b0;
                state <= ST_ACT;
            end
            ST_ACT: begin
                sdram_cke <= 1'b1;
                sdram_ba <= held_addr[12:11];
                sdram_a <= {2'b00, held_addr[10:0]};
                sdram_ncas <= 1'b0;
                sdram_nwe <= writing ? 1'b0 : 1'b1;
                dq_out <= held_data;
                dq_oe <= writing;
                state <= ST_RW;
            end
            ST_RW: begin
                sdram_cke <= 1'b1;
                dq_out <= held_data;
                dq_oe <= writing;
                state <= writing ? ST_HOLD : ST_CAP;
                wait_count <= 13'd0;
            end
            ST_HOLD: begin
                sdram_cke <= 1'b1;
                state <= ST_PRE2;
            end
            ST_CAP: begin
                sdram_cke <= 1'b1;
                if (wait_count == 13'd2) begin
                    rdata <= dq_in;
                    state <= ST_PRE2;
                end else begin
                    wait_count <= wait_count + 13'd1;
                end
            end
            ST_FINISH: begin
                sdram_cke <= 1'b1;
                sdram_ncs <= 1'b1;
                done <= 1'b1;
                state <= ST_IDLE;
            end
            default: begin
                sdram_cke <= 1'b1;
                state <= ST_IDLE;
            end
        endcase
    end
endmodule
