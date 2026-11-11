// S1c trial model, not production code: a behavioural stand-in for Intel's cyclonev_mac
// (which only wraps the encrypted cyclonev_mac_encrypted) covering exactly the
// configurations Quartus 17.0.2 inferred in the fes_i386_cpu and sms_machine netlists:
// unregistered m9x9, m18x18_full and m18x18_plus36. Anything else stops the simulation.
//
// m18x18_plus36 is modelled as ax*ay + {bx, by}, with {bx, by} sign-extended when
// signed_mbx is "true", and loadconst as adding 2**load_const_value. Those readings come from the Cyclone V DSP documentation and from
// how the two netlists wire their multiply-adds (the i386 governor, the Coleco VDP sprite
// x position); they have not been compared with Intel's encrypted model.
`timescale 1 ps / 1 ps
module cyclonev_mac (
    input sub, negate, accumulate, loadconst,
    input [ax_width-1:0] ax,
    input [ay_scan_in_width-1:0] ay,
    input [ay_scan_in_width-1:0] scanin,
    input [az_width-1:0] az,
    input [bx_width-1:0] bx,
    input [by_width-1:0] by,
    input [bz_width-1:0] bz,
    input [2:0] coefsela, coefselb, clk, ena,
    input [1:0] aclr,
    input [63:0] chainin,
    output [result_a_width-1:0] resulta,
    output [result_b_width-1:0] resultb,
    output [scan_out_width-1:0] scanout,
    output [63:0] chainout,
    output dftout
);
    parameter ax_width = 16;
    parameter ay_scan_in_width = 16;
    parameter az_width = 1;
    parameter bx_width = 16;
    parameter by_width = 16;
    parameter bz_width = 1;
    parameter scan_out_width = 1;
    parameter result_a_width = 33;
    parameter result_b_width = 1;
    parameter operation_mode = "m18x18_sumof2";
    parameter mode_sub_location = 0;
    parameter operand_source_max = "input";
    parameter operand_source_may = "input";
    parameter operand_source_mbx = "input";
    parameter operand_source_mby = "input";
    parameter preadder_subtract_a = "false";
    parameter preadder_subtract_b = "false";
    parameter signed_max = "false";
    parameter signed_may = "false";
    parameter signed_mbx = "false";
    parameter signed_mby = "false";
    parameter ay_use_scan_in = "false";
    parameter by_use_scan_in = "false";
    parameter delay_scan_out_ay = "false";
    parameter delay_scan_out_by = "false";
    parameter use_chainadder = "false";
    parameter enable_double_accum = "false";
    parameter [5:0] load_const_value = 6'b0;
    parameter signed [26:0] coef_a_0 = 0, coef_a_1 = 0, coef_a_2 = 0, coef_a_3 = 0;
    parameter signed [26:0] coef_a_4 = 0, coef_a_5 = 0, coef_a_6 = 0, coef_a_7 = 0;
    parameter signed [17:0] coef_b_0 = 0, coef_b_1 = 0, coef_b_2 = 0, coef_b_3 = 0;
    parameter signed [17:0] coef_b_4 = 0, coef_b_5 = 0, coef_b_6 = 0, coef_b_7 = 0;
    parameter ax_clock = "none";
    parameter ay_scan_in_clock = "none";
    parameter az_clock = "none";
    parameter bx_clock = "none";
    parameter by_clock = "none";
    parameter bz_clock = "none";
    parameter coef_sel_a_clock = "none";
    parameter coef_sel_b_clock = "none";
    parameter sub_clock = "none";
    parameter negate_clock = "none";
    parameter accumulate_clock = "none";
    parameter load_const_clock = "none";
    parameter output_clock = "none";

    localparam plus36 = operation_mode == "m18x18_plus36";
    localparam supported =
        (operation_mode == "m9x9" || operation_mode == "m18x18_full" || plus36)
        && operand_source_max == "input" && operand_source_may == "input"
        && operand_source_mbx == "input" && operand_source_mby == "input"
        && ay_use_scan_in == "false" && by_use_scan_in == "false"
        && use_chainadder == "false" && enable_double_accum == "false"
        && (plus36 || signed_mbx == "false") && signed_mby == "false"
        && ax_clock == "none" && ay_scan_in_clock == "none" && bx_clock == "none"
        && by_clock == "none" && sub_clock == "none" && negate_clock == "none"
        && accumulate_clock == "none" && load_const_clock == "none" && output_clock == "none";
    initial if (!supported) begin
        $fatal(1, "cyclonev_mac_fes: unsupported configuration %m (%0s)", operation_mode);
    end

    wire signed [63:0] x = (signed_max == "true") ? 64'($signed(ax)) : 64'($unsigned(ax));
    wire signed [63:0] y = (signed_may == "true") ? 64'($signed(ay)) : 64'($unsigned(ay));
    wire [bx_width+by_width-1:0] b = {bx, by};
    wire [63:0] addend = !plus36 ? 64'd0 : (signed_mbx == "true") ? 64'($signed(b)) : 64'($unsigned(b));
    wire [63:0] result = x * y + addend + (loadconst ? 64'd1 << load_const_value : 64'd0);
    assign resulta = result[result_a_width-1:0];
    assign resultb = '0;
    assign scanout = '0;
    assign chainout = 64'd0;
    assign dftout = 1'b0;
    wire unused = &{1'b0, sub, negate, accumulate, scanin, az, bz, coefsela, coefselb,
                    clk, ena, aclr, chainin};
endmodule
