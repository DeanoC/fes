module fes_sh4_fp_add_tb(input logic clk, output logic finished);
    logic reset = 1, start = 0;
    logic wide, subtract, toward_zero, flush_denormals;
    logic [63:0] lhs, rhs, value;
    logic [5:0] causes;
    logic done, unused_normal_operands;

    fes_sh4_fp_add dut (.normal_operands(unused_normal_operands), .*);

    task automatic check(
        input logic [3:0] mode,
        input logic [63:0] a, b, expected,
        input logic [5:0] expected_causes
    );
        @(negedge clk);
        {wide, subtract, toward_zero, flush_denormals} = mode;
        lhs = a;
        rhs = b;
        start = 1;
        @(negedge clk);
        start = 0;
        wait (done);
        assert (causes == expected_causes && (expected_causes[5] || value == expected))
            else $fatal(1, "FP add mode %h %h %h: got %h/%h expected %h/%h",
                        mode, a, b, value, causes, expected, expected_causes);
        wait (!done);
    endtask

    initial begin
        finished = 0;
        @(negedge clk);
        reset = 0;
        check(0, 64'h3f80_0000, 64'h3f80_0000, 64'h4000_0000, 0);
        check(4, 64'h3f80_0000, 64'hbfc0_0000, 64'h4020_0000, 0);
        check(4, 64'h3f80_0001, 64'h3f80_0000, 64'h3400_0000, 0);
        check(0, 64'h3f80_0000, 64'h3380_0000, 64'h3f80_0000, 1);
        check(0, 64'h3f80_0001, 64'h3380_0000, 64'h3f80_0002, 1);
        check(2, 64'h3f80_0001, 64'h3380_0000, 64'h3f80_0001, 1);
        check(0, 64'h3fff_ffff, 64'h3380_0000, 64'h4000_0000, 1);
        check(4, 64'h3f80_0000, 64'h0080_0000, 64'h3f80_0000, 1);
        check(6, 64'h3f80_0000, 64'h0080_0000, 64'h3f7f_ffff, 1);
        check(0, 64'h8000_0000, 64'h8000_0000, 64'h8000_0000, 0);
        check(4, 64'h8000_0000, 64'h8000_0000, 64'h0000_0000, 0);
        check(4, 64'h0080_0001, 64'h0080_0000, 64'h0000_0001, 0);
        check(5, 64'h8080_0001, 64'h8080_0000, 64'h8000_0000, 3);
        check(0, 64'h0000_0001, 64'h3f80_0000, 0, 32);
        check(1, 64'h0000_0001, 64'h3f80_0000, 64'h3f80_0000, 0);
        check(0, 64'h7f7f_ffff, 64'h7f7f_ffff, 64'h7f80_0000, 5);
        check(2, 64'hff7f_ffff, 64'hff7f_ffff, 64'hff7f_ffff, 5);
        check(0, 64'h7f80_0000, 64'hff80_0000, 64'h7fbf_ffff, 16);
        check(0, 64'h7fc0_0000, 64'h3f80_0000, 64'h7fbf_ffff, 16);
        check(0, 64'h7f80_0001, 64'h0000_0001, 64'h7fbf_ffff, 0);
        check(8, 64'h3ff8_0000_0000_0000, 64'h4002_0000_0000_0000, 64'h400e_0000_0000_0000, 0);
        check(12, 64'h3ff0_0000_0000_0001, 64'h3ff0_0000_0000_0000, 64'h3cb0_0000_0000_0000, 0);
        check(8, 64'h3ff0_0000_0000_0001, 64'h3ca0_0000_0000_0000, 64'h3ff0_0000_0000_0002, 1);
        check(10, 64'h3ff0_0000_0000_0001, 64'h3ca0_0000_0000_0000, 64'h3ff0_0000_0000_0001, 1);
        check(12, 64'h0010_0000_0000_0001, 64'h0010_0000_0000_0000, 64'h0000_0000_0000_0001, 0);
        check(8, 64'h7fef_ffff_ffff_ffff, 64'h7fef_ffff_ffff_ffff, 64'h7ff0_0000_0000_0000, 5);
        check(8, 64'h7ff8_0000_0000_0000, 64'h3ff0_0000_0000_0000, 64'h7ff7_ffff_ffff_ffff, 16);
        finished = 1;
    end
endmodule
