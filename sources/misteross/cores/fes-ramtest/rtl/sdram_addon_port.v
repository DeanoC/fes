// SPDX-License-Identifier: GPL-2.0-or-later
// 16-bit SDR SDRAM, burst length 1, CAS latency 2.
// 128 MB is 64M halfwords on two chips. The MiSTer memory tester packs a
// halfword as column[1:0], bank, row[12:0], column[9:2], and uses the top
// bit as the chip select. A10 is auto-precharge on READ and WRITE.
// A refresh is inserted between commands so a full-chip scan keeps its data.
module sdram_addon_port (
    input wire clk,
    input wire clk_pin,
    input wire [1:0] rate,
    input wire reset,
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
    output wire [15:0] dq_out,
    output reg dq_oe,
    // Both edges are captured in the IO cell. The rising-edge word is
    // from the previous rising edge, because that register updates then.
    input wire [15:0] dq_rise,
    input wire [15:0] dq_fall
);
    localparam [4:0] ST_BOOT = 5'd0;
    localparam [4:0] ST_PRE = 5'd1;
    localparam [4:0] ST_REF1 = 5'd2;
    localparam [4:0] ST_REF2 = 5'd3;
    localparam [4:0] ST_MRS = 5'd4;
    localparam [4:0] ST_IDLE = 5'd5;
    localparam [4:0] ST_REF = 5'd6;
    localparam [4:0] ST_REFW = 5'd7;
    localparam [4:0] ST_ROW = 5'd8;
    localparam [4:0] ST_ACT = 5'd9;
    localparam [4:0] ST_RW = 5'd10;
    localparam [4:0] ST_HOLD = 5'd11;
    localparam [4:0] ST_CAP = 5'd12;
    localparam [4:0] ST_PRE2 = 5'd13;
    localparam [4:0] ST_FINISH = 5'd14;
    localparam [4:0] ST_RCD1 = 5'd15;
    localparam [4:0] ST_RCD2 = 5'd16;

    reg [4:0] state = ST_BOOT;
    reg [13:0] wait_count = 14'd0;
    reg [11:0] refresh_div = 12'd0;
    reg refresh_due = 1'b0;
    reg seen = 1'b0;
    reg writing = 1'b0;
    reg [25:0] held_addr = 26'd0;
    reg [15:0] held_data = 16'h0000;
    reg [15:0] dq_out_q;
`ifdef RAM_OSS_HIGH_SPEED
    // held_data is latched with the request, before the SDRAM write edge.
    // Keep the write word out of the controller's state/output mux.
    assign dq_out = held_data;
`else
    assign dq_out = dq_out_q;
`endif
    reg init_hi = 1'b0;
    reg ref_hi = 1'b0;
`ifdef RAM_OSS_HIGH_SPEED
    reg capture_due = 1'b0;
`endif

    // Same polarity as the Sorgelig controller: the pin rises on the FPGA
    // falling edge, so the chip samples commands a half-cycle after they launch.
    altddio_out #(
        .width(1),
        .intended_device_family("Cyclone V"),
        .power_up_high("OFF"),
        .oe_reg("UNREGISTERED"),
        .extend_oe_disable("OFF"),
        .invert_output("OFF")
    ) sdram_clk_ddr (
        .datain_h(1'b0),
        .datain_l(1'b1),
        .outclock(clk_pin),
        .outclocken(1'b1),
        .aset(1'b0),
        .aclr(1'b0),
        .sset(1'b0),
        .sclr(1'b0),
        .oe(1'b1),
        .dataout(sdram_clk),
        .oe_out()
    );

    // 7.8 us refresh. The count is in fabric clocks, so it tracks the rate.
    wire [11:0] refresh_every =
        rate == 2'd0 ? 12'd390 :
        rate == 2'd1 ? 12'd1014 :
        12'd780;

    always @(posedge clk) begin
`ifdef RAM_OSS_HIGH_SPEED
        capture_due <= (rate == 2'd1 && state == ST_CAP && wait_count == 14'd4);
        if (capture_due)
            rdata <= dq_fall;
`endif
        done <= 1'b0;
        dq_oe <= 1'b0;
        sdram_dqml <= 1'b0;
        sdram_dqmh <= 1'b0;
        sdram_ncs <= 1'b0;
        sdram_nras <= 1'b1;
        sdram_ncas <= 1'b1;
        sdram_nwe <= 1'b1;
        if (refresh_div == refresh_every) begin
            refresh_div <= 12'd0;
            refresh_due <= 1'b1;
        end else begin
            refresh_div <= refresh_div + 12'd1;
        end
        if (reset) begin
            state <= ST_BOOT;
            wait_count <= 14'd0;
            refresh_due <= 1'b0;
            seen <= 1'b0;
            init_hi <= 1'b0;
            ref_hi <= 1'b0;
            sdram_cke <= 1'b0;
        end else case (state)
            ST_BOOT: begin
                sdram_cke <= 1'b0;
                sdram_ncs <= 1'b1;
                // At least 100 us at 130 MHz.
                if (wait_count == 14'd13000) begin
                    sdram_cke <= 1'b1;
                    state <= ST_PRE;
                    wait_count <= 14'd0;
                end else begin
                    wait_count <= wait_count + 14'd1;
                end
            end
            ST_PRE: begin
                sdram_cke <= 1'b1;
                if (wait_count == 14'd0) begin
                    sdram_ncs <= init_hi;
                    sdram_nras <= 1'b0;
                    sdram_nwe <= 1'b0;
                    sdram_a[10] <= 1'b1;
                    wait_count <= 14'd1;
                end else if (wait_count == 14'd4) begin
                    wait_count <= 14'd0;
                    if (!init_hi)
                        init_hi <= 1'b1;
                    else begin
                        init_hi <= 1'b0;
                        state <= ST_REF1;
                    end
                end else begin
                    wait_count <= wait_count + 14'd1;
                end
            end
            ST_PRE2: begin
                sdram_cke <= 1'b1;
                sdram_ncs <= held_addr[25];
                sdram_nras <= 1'b0;
                sdram_nwe <= 1'b0;
                sdram_a[10] <= 1'b1;
                state <= ST_FINISH;
            end
            ST_REF1, ST_REF2: begin
                sdram_cke <= 1'b1;
                if (wait_count == 14'd0) begin
                    sdram_ncs <= init_hi;
                    sdram_nras <= 1'b0;
                    sdram_ncas <= 1'b0;
                    wait_count <= 14'd1;
                // 16 cycles is 160 ns at 100 MHz, inside tRFC, and longer when slower.
                end else if (wait_count == 14'd16) begin
                    wait_count <= 14'd0;
                    if (state == ST_REF1)
                        state <= ST_REF2;
                    else if (!init_hi) begin
                        init_hi <= 1'b1;
                        state <= ST_REF1;
                    end else begin
                        init_hi <= 1'b0;
                        state <= ST_MRS;
                    end
                end else begin
                    wait_count <= wait_count + 14'd1;
                end
            end
            ST_MRS: begin
                sdram_cke <= 1'b1;
                if (wait_count == 14'd0) begin
                    sdram_ncs <= init_hi;
                    sdram_nras <= 1'b0;
                    sdram_ncas <= 1'b0;
                    sdram_nwe <= 1'b0;
                    sdram_ba <= 2'b00;
                    // CAS latency 3 at 130 MHz; 2 at the slower rates.
                    // Burst length one throughout.
                    sdram_a <= (rate == 2'd1) ? 13'h0030 : 13'h0020;
                    wait_count <= 14'd1;
                end else if (wait_count == 14'd4) begin
                    wait_count <= 14'd0;
                    if (!init_hi)
                        init_hi <= 1'b1;
                    else begin
                        init_hi <= 1'b0;
                        state <= ST_IDLE;
                    end
                end else begin
                    wait_count <= wait_count + 14'd1;
                end
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
                        sdram_ba <= addr[3:2];
                        sdram_a <= addr[16:4];
                        sdram_ncs <= addr[25];
                        sdram_nras <= 1'b0;
                        state <= ST_RCD1;
                        // Data has to be valid before the write clock, not on it.
                        if (write) begin
                            dq_out_q <= wdata;
                            dq_oe <= 1'b1;
                        end
                    end
                end
            end
            ST_REF: begin
                sdram_cke <= 1'b1;
                sdram_ncs <= ref_hi;
                sdram_nras <= 1'b0;
                sdram_ncas <= 1'b0;
                refresh_due <= 1'b0;
                wait_count <= 14'd0;
                state <= ST_REFW;
            end
            ST_REFW: begin
                sdram_cke <= 1'b1;
                sdram_ncs <= ref_hi;
                if (wait_count == 14'd16) begin
                    if (!ref_hi) begin
                        ref_hi <= 1'b1;
                        state <= ST_REF;
                    end else begin
                        ref_hi <= 1'b0;
                        state <= ST_ROW;
                    end
                end else begin
                    wait_count <= wait_count + 14'd1;
                end
            end
            ST_ROW: begin
                sdram_cke <= 1'b1;
                sdram_ncs <= held_addr[25];
                sdram_ba <= held_addr[3:2];
                sdram_a <= held_addr[16:4];
                sdram_nras <= 1'b0;
                state <= ST_RCD1;
                if (writing) begin
                    dq_out_q <= held_data;
                    dq_oe <= 1'b1;
                end
            end
            ST_RCD1, ST_RCD2: begin
                sdram_cke <= 1'b1;
                dq_out_q <= held_data;
                dq_oe <= writing;
                state <= (state == ST_RCD1) ? ST_RCD2 : ST_ACT;
            end
            ST_ACT: begin
                sdram_cke <= 1'b1;
                sdram_ncs <= held_addr[25];
                sdram_ba <= held_addr[3:2];
                // A10 is auto-precharge. Column is {addr[24:17], addr[1:0]}.
                sdram_a <= {2'b00, 1'b1, held_addr[24:17], held_addr[1:0]};
                sdram_ncas <= 1'b0;
                sdram_nwe <= writing ? 1'b0 : 1'b1;
                dq_out_q <= held_data;
                dq_oe <= writing;
                state <= ST_RW;
            end
            ST_RW: begin
                sdram_cke <= 1'b1;
                dq_out_q <= held_data;
                dq_oe <= writing;
                state <= writing ? ST_HOLD : ST_CAP;
                wait_count <= 14'd0;
            end
            ST_HOLD: begin
                sdram_cke <= 1'b1;
                state <= ST_PRE2;
            end
            ST_CAP: begin
                sdram_cke <= 1'b1;
                // READ is on the pins in ST_ACT. The chip samples it on the
                // falling edge and launches the word two chip clocks later.
                // 50 MHz keeps the rising edge 10 ns after that launch.
                // 75 and 100 MHz keep the falling edge one chip clock after
                // the launch. At 100 MHz the capture clock leads by 0.42 ns.
                if (wait_count == (rate == 2'd1 ?
`ifdef RAM_OSS_HIGH_SPEED
                                   14'd5 :
`else
                                   14'd4 :
`endif
                                   rate == 2'd2 ? 14'd2 : 14'd3)) begin
`ifdef RAM_OSS_HIGH_SPEED
                    if (rate != 2'd1)
                        rdata <= (rate == 2'd0) ? dq_rise : dq_fall;
`else
                    rdata <= (rate == 2'd0) ? dq_rise : dq_fall;
`endif
                    state <= ST_PRE2;
                end else begin
                    wait_count <= wait_count + 14'd1;
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
