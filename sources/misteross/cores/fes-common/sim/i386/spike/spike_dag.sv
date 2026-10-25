// SPDX-License-Identifier: MIT
// Timing spike (b): the D and AG stages for an address. D decodes the
// address registers from ModRM and SIB (fes_i386_amode), reads the
// flip-flop register file and forwards from the M1, M2, EX and WB result
// registers by comparing their destination tags (fes_i386_dfwd), scales the
// index, picks the segment, and registers AG's operands. AG takes at most one
// late value from the M1, EX or WB register and runs fes_i386_agu into the
// M1 register. The stimulus stands in for Q, the register file writes, the
// segment caches and the uops behind AG; AG's result feeds the M1 register
// as an early-ALU result would.
module spike_dag (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic         reset;
    logic [255:0] noise;
    spike_stimulus #(.WIDTH(256)) stimulus (.clk, .key0_n(KEY0_N), .reset, .noise);

    // Q/D register and D's state.
    logic [7:0]       modrm, sib;
    logic             addr32;
    logic             seg_override;
    logic [2:0]       seg_named;
    logic [31:0]      disp;
    logic [7:0][31:0] gpr;
    logic [5:0][31:0] seg_base;
    logic [5:0][15:0] seg_hi_plus1;
    logic [4:0]       prod_valid, prod_ready, prod_late;
    logic [4:0][2:0]  prod_dst;
    // Result registers behind AG.
    logic [31:0]      m1_result, m2_result, ex_result, wb_result;

    always_ff @(posedge clk) begin
        modrm <= noise[7:0];
        sib <= noise[15:8];
        addr32 <= noise[16];
        seg_override <= noise[17];
        seg_named <= noise[20:18];
        disp <= noise[52:21];
        gpr[noise[55:53]] <= noise[87:56];
        seg_base[noise[90:88] % 6] <= noise[122:91];
        seg_hi_plus1[noise[90:88] % 6] <= noise[122:107] + 16'd1;
        prod_valid <= noise[127:123];
        prod_ready <= noise[132:128];
        prod_late <= noise[137:133];
        prod_dst <= noise[152:138];
    end

    // ---- D --------------------------------------------------------------------
    logic        has_base, has_index, stack, stall;
    logic [2:0]  base, index, segment;
    logic [1:0]  scale, late, late_hi;
    logic [31:0] row, last;

    fes_i386_amode amode (
        .modrm, .sib, .addr32, .has_base, .base, .has_index, .index, .scale, .stack
    );
    fes_i386_dfwd dfwd (
        .has_base, .base, .has_index, .index, .scale, .addr32, .gpr, .prod_valid, .prod_dst,
        .prod_ready, .prod_late, .stage_value({wb_result, ex_result, m2_result, m1_result}),
        .row, .last, .late, .late_hi, .stall
    );
    assign segment = seg_override ? (seg_named > 3'd5 ? 3'd3 : seg_named) : stack ? 3'd2 : 3'd3;

    // D/AG register.
    logic        ag_valid, ag_addr32;
    logic [31:0] ag_row, ag_last, ag_disp, ag_seg_term;
    logic [15:0] ag_seg_hi, ag_seg_hi_plus1;
    logic [1:0]  ag_late, ag_late_hi;

    always_ff @(posedge clk) begin
        ag_valid <= !stall;
        ag_addr32 <= addr32;
        ag_row <= row;
        ag_last <= last;
        ag_late <= late;
        ag_late_hi <= late_hi;
        ag_disp <= addr32 ? disp : {16'b0, disp[15:0]};
        ag_seg_term <= addr32 ? seg_base[segment] : {16'b0, seg_base[segment][15:0]};
        ag_seg_hi <= seg_base[segment][31:16];
        ag_seg_hi_plus1 <= seg_hi_plus1[segment];
    end

    // ---- AG -------------------------------------------------------------------
    logic [31:0] linear, offset;

    fes_i386_agu agu (
        .addr32(ag_addr32), .seg_term(ag_seg_term), .seg_hi(ag_seg_hi), .seg_hi_plus1(ag_seg_hi_plus1),
        .row(ag_row), .last(ag_last), .disp(ag_disp), .late(ag_late), .late_hi(ag_late_hi),
        .late_m1(m1_result), .late_ex(ex_result), .late_wb(wb_result), .linear, .offset
    );

    logic        m1_valid;
    logic [31:0] m1_linear, m1_offset;
    always_ff @(posedge clk) begin
        m1_valid <= ag_valid;
        m1_linear <= linear;
        m1_offset <= offset;
        m1_result <= linear;
        m2_result <= m1_linear ^ m1_offset;
        ex_result <= m2_result ^ noise[191:160];
        wb_result <= ex_result ^ noise[223:192];
    end

    logic [31:0] signature;
    always_ff @(posedge clk) begin
        signature <= {signature[30:0], signature[31]} ^ m1_linear ^ m1_offset ^ wb_result ^ {31'b0, m1_valid};
        LED0 <= ^signature;
    end
endmodule
