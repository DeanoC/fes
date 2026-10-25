// SPDX-License-Identifier: MIT
// Timing spike (b): the AG stage. D hands AG its register-file operands, the
// displacement, the segment base and, already compared, the forwarding
// selects; AG takes base and index from a 5:1 forwarding mux (register file,
// M1, M2, EX, WB results) and the segment base from the segment cache or a
// real-mode segment write in M1, then fes_i386_agu. Its outputs are the M1
// pipeline register. The later results stand in for the pipeline behind AG.
module spike_ag (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic         reset;
    logic [255:0] noise;
    spike_stimulus #(.WIDTH(256)) stimulus (.clk, .key0_n(KEY0_N), .reset, .noise);

    // D/AG pipeline register.
    logic [31:0] d_base, d_index, d_disp, d_seg_base;
    logic [15:0] d_seg_hi_plus1;
    logic [1:0]  d_scale;
    logic        d_addr32;
    logic [2:0]  d_sel_base, d_sel_index;  // 0 register file, 1 M1, 2 M2, 3 EX, 4 WB
    logic        d_sel_seg;                // 0 segment cache, 1 segment write in M1

    // Results in flight behind AG.
    logic [31:0] m1_linear, m1_offset, m1_result, m2_result, ex_result, wb_result;
    logic [31:0] m1_seg_base;
    logic [15:0] m1_seg_hi_plus1;

    always_ff @(posedge clk) begin
        d_base <= noise[31:0];
        d_index <= noise[63:32];
        d_disp <= noise[95:64];
        d_seg_base <= noise[127:96];
        d_seg_hi_plus1 <= noise[127:112] + 16'd1;
        d_scale <= noise[129:128];
        d_addr32 <= noise[130];
        d_sel_base <= noise[133:131] > 3'd4 ? 3'd0 : noise[133:131];
        d_sel_index <= noise[136:134] > 3'd4 ? 3'd0 : noise[136:134];
        d_sel_seg <= noise[137];
    end

    function automatic logic [31:0] forward(input logic [2:0] sel, input logic [31:0] file,
                                            input logic [31:0] m1, input logic [31:0] m2,
                                            input logic [31:0] ex, input logic [31:0] wb);
        case (sel)
            3'd1: forward = m1;
            3'd2: forward = m2;
            3'd3: forward = ex;
            3'd4: forward = wb;
            default: forward = file;
        endcase
    endfunction

    logic [31:0] base, index, seg_base, linear, offset;
    logic [15:0] seg_hi_plus1;

    assign base = forward(d_sel_base, d_base, m1_result, m2_result, ex_result, wb_result);
    assign index = forward(d_sel_index, d_index, m1_result, m2_result, ex_result, wb_result);
    assign seg_base = d_sel_seg ? m1_seg_base : d_seg_base;
    assign seg_hi_plus1 = d_sel_seg ? m1_seg_hi_plus1 : d_seg_hi_plus1;

    fes_i386_agu agu (
        .addr32(d_addr32), .seg_base, .seg_base_hi_plus1(seg_hi_plus1), .base, .index,
        .scale(d_scale), .disp(d_disp), .linear, .offset
    );

    always_ff @(posedge clk) begin
        m1_linear <= linear;
        m1_offset <= offset;
        // A real-mode segment load from a register: base = selector * 16,
        // computed beside the address from the forwarded operand.
        m1_seg_base <= {12'b0, base[15:0], 4'b0};
        m1_seg_hi_plus1 <= {12'b0, base[15:12]} + 16'd1;
        m1_result <= linear ^ noise[223:192];
        m2_result <= m1_result ^ m1_offset;
        ex_result <= m2_result ^ noise[255:224];
        wb_result <= ex_result;
    end

    logic [31:0] signature;
    always_ff @(posedge clk) begin
        signature <= {signature[30:0], signature[31]} ^ m1_linear ^ m1_offset ^ wb_result
                   ^ m1_seg_base ^ {16'b0, m1_seg_hi_plus1};
        LED0 <= ^signature;
    end
endmodule
