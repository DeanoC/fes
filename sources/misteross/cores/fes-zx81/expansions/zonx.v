// SPDX-License-Identifier: GPL-2.0-or-later
// Bi-Pak Zon X-81 register file on the freeze-scaffold Z80-like edge.
// I/O decode matches (port & 008F): select xxDF/xxCF, data xx0F.
// Channel A is a digital square (R0/R1 period, R7 tone enable, R8 level)
// returned on peek_d; the shell mixes that into HDMI I2S. Data-port reads
// remain an FPGA diagnostic for the two-cycle file.
`include "zx81_bus_pack.vh"
module cart (
    input wire FPGA_CLK1_50,
    input wire [`ZX81_BUS_REQ-1:0] plug_addr,
    output wire [`ZX81_BUS_RSP-1:0] plug_rdata
);
    wire [15:0] cpu_a = plug_addr[`ZX81_BUS_A];
    wire [7:0] cpu_d = plug_addr[`ZX81_BUS_DWR];
    wire io_we = !plug_addr[`ZX81_BUS_IORQ_N] && !plug_addr[`ZX81_BUS_WR_N];
    wire io_rd = !plug_addr[`ZX81_BUS_IORQ_N] && !plug_addr[`ZX81_BUS_RD_N];
    wire sel_reg = io_we && ((cpu_a[7:0] & 8'h8f) == 8'h8f);
    wire sel_data = io_we && ((cpu_a[7:0] & 8'h8f) == 8'h0f);
    wire sel_read = io_rd && ((cpu_a[7:0] & 8'h8f) == 8'h0f);
    wire [7:0] data_q;
    wire [3:0] ay_sel;
    wire [7:0] tone_fine;
    wire [7:0] tone_coarse;
    wire [7:0] mixer;
    wire [7:0] level_a;
`ifdef SYNTHESIS
    // Same TDP A1WE overlay as the 16K pack / QS window. Mixed-width A1EN
    // held on the 904 HPS bench but CPU OUT pulses left the file at 0.
    (* keep *) wire [9:0] sel_rd = {
        cpu_a[9] & cpu_a[7],
        cpu_a[8] & cpu_a[7],
        cpu_a[7] | cpu_a[0],
        cpu_a[6] | cpu_a[0],
        cpu_a[5],
        cpu_a[4] | cpu_a[0],
        cpu_a[3:0]
    };
    wire [9:0] sel_q;
    wire [9:0] data_q10;
    assign ay_sel = sel_q[3:0];
    (* keep *) wire [9:0] data_addr = {cpu_a[9:8], ay_sel, cpu_a[3:0]};
    (* keep, BEL = "MISTRAL_M10K.26.2.0" *)
    MISTRAL_M10K_TDP #(
        .CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1)
    ) ay_sel_cell (
        .CLK1(FPGA_CLK1_50), .CLK2(FPGA_CLK1_50),
        .A1ADDR(cpu_a[9:0]),
        .B1ADDR(sel_rd),
        .A1DATA({6'b0, cpu_d[3:0]}),
        .B1DATA(10'b0),
        .A1Q(),
        .B1Q(sel_q),
        .A1EN(1'b1),
        .B1EN(1'b1),
        .A1WE(sel_reg),
        .B1WE(1'b0),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.1.0" *)
    MISTRAL_M10K_TDP #(
        .CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1)
    ) ay_regs (
        .CLK1(FPGA_CLK1_50), .CLK2(FPGA_CLK1_50),
        .A1ADDR(data_addr),
        .B1ADDR(data_addr),
        .A1DATA({2'b0, cpu_d}),
        .B1DATA(10'b0),
        .A1Q(),
        .B1Q(data_q10),
        .A1EN(1'b1),
        .B1EN(1'b1),
        .A1WE(sel_data),
        .B1WE(1'b0),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    assign data_q = data_q10[7:0];
    // M10K cannot expose R0/R1/R7/R8 together; shadow the tone file in FFs.
    reg [7:0] r0, r1, r7, r8;
    initial begin
        r0 = 8'h00;
        r1 = 8'h00;
        r7 = 8'h3f;
        r8 = 8'h00;
    end
    always @(posedge FPGA_CLK1_50) begin
        if (sel_data) begin
            case (ay_sel)
                4'd0: r0 <= cpu_d;
                4'd1: r1 <= cpu_d;
                4'd7: r7 <= cpu_d;
                4'd8: r8 <= cpu_d;
                default: ;
            endcase
        end
    end
    assign tone_fine = r0;
    assign tone_coarse = r1;
    assign mixer = r7;
    assign level_a = r8;
`else
    reg [3:0] sel;
    reg [7:0] regs [0:15];
    integer i;
    initial begin
        sel = 4'd0;
        for (i = 0; i < 16; i = i + 1) regs[i] = 8'h00;
    end
    always @(posedge FPGA_CLK1_50) begin
        if (sel_reg) sel <= cpu_d[3:0];
        if (sel_data) regs[sel] <= cpu_d;
    end
    assign ay_sel = sel;
    assign data_q = regs[sel];
    assign tone_fine = regs[0];
    assign tone_coarse = regs[1];
    assign mixer = regs[7];
    assign level_a = regs[8];
`endif
    // Nested 4-bit down-counters, not a 12-bit subtract. Yosys mapped the
    // wide phase counter to ALUT_ARITH carry that collided in the slot
    // (`WIRE.25.3.CARRY`). Reload is {R1[3:0], R0[3:0]} after /16 prescale.
    (* keep *) reg [3:0] tick;
    (* keep *) reg [3:0] lo;
    (* keep *) reg [3:0] hi;
    (* keep *) reg square;
    initial begin
        tick = 4'd0;
        lo = 4'd0;
        hi = 4'd0;
        square = 1'b0;
    end
    wire [3:0] load_lo = tone_fine[3:0];
    wire [3:0] load_hi = tone_coarse[3:0];
    function [3:0] inc4;
        input [3:0] value;
        begin
            case (value)
                4'h0: inc4 = 4'h1;
                4'h1: inc4 = 4'h2;
                4'h2: inc4 = 4'h3;
                4'h3: inc4 = 4'h4;
                4'h4: inc4 = 4'h5;
                4'h5: inc4 = 4'h6;
                4'h6: inc4 = 4'h7;
                4'h7: inc4 = 4'h8;
                4'h8: inc4 = 4'h9;
                4'h9: inc4 = 4'ha;
                4'ha: inc4 = 4'hb;
                4'hb: inc4 = 4'hc;
                4'hc: inc4 = 4'hd;
                4'hd: inc4 = 4'he;
                4'he: inc4 = 4'hf;
                default: inc4 = 4'h0;
            endcase
        end
    endfunction
    function [3:0] dec4;
        input [3:0] value;
        begin
            case (value)
                4'h0: dec4 = 4'hf;
                4'h1: dec4 = 4'h0;
                4'h2: dec4 = 4'h1;
                4'h3: dec4 = 4'h2;
                4'h4: dec4 = 4'h3;
                4'h5: dec4 = 4'h4;
                4'h6: dec4 = 4'h5;
                4'h7: dec4 = 4'h6;
                4'h8: dec4 = 4'h7;
                4'h9: dec4 = 4'h8;
                4'ha: dec4 = 4'h9;
                4'hb: dec4 = 4'ha;
                4'hc: dec4 = 4'hb;
                4'hd: dec4 = 4'hc;
                4'he: dec4 = 4'hd;
                default: dec4 = 4'he;
            endcase
        end
    endfunction
    always @(posedge FPGA_CLK1_50) begin
        tick <= inc4(tick);
        if (tick == 4'd0) begin
            if (lo == 4'd0 && hi == 4'd0) begin
                lo <= (load_lo == 4'd0 && load_hi == 4'd0) ? 4'd1 : load_lo;
                hi <= load_hi;
                square <= ~square;
            end else if (lo == 4'd0) begin
                lo <= 4'hf;
                hi <= dec4(hi);
            end else begin
                lo <= dec4(lo);
            end
        end
    end
    wire tone_on = !mixer[0];
    wire [7:0] pcm = (tone_on && square) ? {level_a[3:0], 4'b0} : 8'h00;
    assign plug_rdata = {
        1'b0, 1'b0, 1'b0, sel_read,
        pcm, data_q
    };
endmodule
