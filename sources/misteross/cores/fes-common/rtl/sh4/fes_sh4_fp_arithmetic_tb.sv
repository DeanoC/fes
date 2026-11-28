module fes_sh4_fp_arithmetic_tb(input logic clk, output logic finished);
    logic reset = 1, start = 0;
    logic wide, multiply = 0, divide = 0, square_root = 0, subtract, toward_zero, flush_denormals;
    logic [2:0] transfer = 0;
    logic [63:0] lhs, rhs, value;
    logic [5:0] causes;
    logic done, unused_normal_operands;

    fes_sh4_fp_arithmetic dut (.normal_operands(unused_normal_operands), .*);

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
            else $fatal(1, "FP arithmetic mode %h %h %h: got %h/%h expected %h/%h",
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
        multiply = 1;
        check(0, 64'h3fc0_0000, 64'h4000_0000, 64'h4040_0000, 0);
        check(0, 64'hbfc0_0000, 64'h4020_0000, 64'hc070_0000, 0);
        check(0, 64'h3f80_0001, 64'h3f80_0001, 64'h3f80_0002, 1);
        check(0, 64'h0080_0000, 64'h3f00_0000, 64'h0040_0000, 0);
        check(1, 64'h8080_0000, 64'h3f00_0000, 64'h8000_0000, 3);
        check(0, 64'h0080_0000, 64'h0080_0000, 0, 3);
        check(0, 64'h0080_0000, 64'h3f7f_ffff, 64'h0080_0000, 1);
        check(2, 64'h0080_0000, 64'h3f7f_ffff, 64'h007f_ffff, 3);
        check(0, 64'h7f7f_ffff, 64'h4000_0000, 64'h7f80_0000, 5);
        check(2, 64'h7f7f_ffff, 64'h4000_0000, 64'h7f7f_ffff, 5);
        check(0, 64'h8000_0000, 64'h3f80_0000, 64'h8000_0000, 0);
        check(0, 64'h8000_0000, 64'h7f80_0000, 64'h7fbf_ffff, 16);
        check(0, 64'hff80_0000, 64'hbf80_0000, 64'h7f80_0000, 0);
        check(0, 64'h0000_0001, 64'hff80_0000, 0, 32);
        check(1, 64'h0000_0001, 64'hff80_0000, 64'h7fbf_ffff, 16);
        check(8, 64'h3ff8_0000_0000_0000, 64'h4002_0000_0000_0000, 64'h400b_0000_0000_0000, 0);
        check(8, 64'h3fff_ffff_ffff_ffff, 64'h3fff_ffff_ffff_ffff, 64'h400f_ffff_ffff_fffe, 1);
        check(8, 64'h0010_0000_0000_0000, 64'h3fe0_0000_0000_0000, 64'h0008_0000_0000_0000, 0);
        check(9, 64'h8010_0000_0000_0000, 64'h3fe0_0000_0000_0000, 64'h8000_0000_0000_0000, 3);
        check(8, 64'h7fef_ffff_ffff_ffff, 64'h4000_0000_0000_0000, 64'h7ff0_0000_0000_0000, 5);
        multiply = 0;
        divide = 1;
        check(0, 64'h3f80_0000, 64'h4040_0000, 64'h3eaa_aaab, 1);
        check(2, 64'h3f80_0000, 64'h4040_0000, 64'h3eaa_aaaa, 1);
        check(0, 64'hbf80_0000, 64'h0000_0000, 64'hff80_0000, 8);
        check(0, 64'h8000_0000, 64'h0000_0000, 64'h7fbf_ffff, 16);
        check(0, 64'h7f80_0000, 64'hff80_0000, 64'h7fbf_ffff, 16);
        check(0, 64'h3f80_0000, 64'hff80_0000, 64'h8000_0000, 0);
        check(0, 64'h7f80_0000, 64'h8000_0000, 64'hff80_0000, 0);
        check(0, 64'h0080_0000, 64'h4000_0000, 64'h0040_0000, 0);
        check(1, 64'h0080_0000, 64'h4000_0000, 0, 3);
        check(0, 64'h7f7f_ffff, 64'h3f00_0000, 64'h7f80_0000, 5);
        check(8, 64'h3ff0_0000_0000_0000, 64'h4008_0000_0000_0000, 64'h3fd5_5555_5555_5555, 1);
        check(8, 64'h400c_0000_0000_0000, 64'h3ff0_0000_0000_0000, 64'h400c_0000_0000_0000, 0);

        divide = 0;
        square_root = 1;
        check(0, 64'h4000_0000, 0, 64'h3fb5_04f3, 1);
        check(0, 64'h3f00_0000, 0, 64'h3f35_04f3, 1);
        check(0, 64'h4080_0000, 0, 64'h4000_0000, 0);
        check(0, 64'hbf80_0000, 0, 64'h7fbf_ffff, 16);
        check(0, 64'h8000_0000, 0, 64'h8000_0000, 0);
        check(0, 64'hff80_0000, 0, 64'h7fbf_ffff, 16);
        check(0, 64'h7f80_0000, 0, 64'h7f80_0000, 0);
        check(0, 64'h0000_0001, 0, 0, 32);
        check(0, 64'h8000_0001, 0, 64'h7fbf_ffff, 16);
        check(1, 64'h8000_0001, 0, 64'h8000_0000, 0);
        check(8, 64'h4000_0000_0000_0000, 0, 64'h3ff6_a09e_667f_3bcd, 1);
        check(10, 64'h4000_0000_0000_0000, 0, 64'h3ff6_a09e_667f_3bcc, 1);
        check(8, 64'h4010_0000_0000_0000, 0, 64'h4000_0000_0000_0000, 0);
        square_root = 0;
        transfer = 1; // FCMP/EQ
        check(0, 64'h8000_0000, 0, 1, 0);
        check(0, 64'h7fbf_ffff, 64'h3f80_0000, 0, 0);
        check(0, 64'h3f80_0000, 64'h7fc0_0000, 0, 16);
        check(0, 64'h0000_0001, 0, 0, 0);
        check(1, 64'h0000_0001, 0, 1, 0);
        check(8, 64'h3ff0_0000_0000_0001, 64'h3ff0_0000_0000_0000, 0, 0);

        transfer = 2; // FCMP/GT
        check(0, 64'h7fbf_ffff, 64'h3f80_0000, 0, 16);
        check(0, 64'hbf80_0000, 64'hc000_0000, 1, 0);
        check(0, 64'h8000_0000, 0, 0, 0);
        check(8, 64'h3ff0_0000_0000_0001, 64'h3ff0_0000_0000_0000, 1, 0);

        transfer = 3; // FLOAT
        check(0, 64'h0100_0001, 0, 64'h4b80_0000, 1);
        check(0, 64'h0100_0003, 0, 64'h4b80_0002, 1);
        check(2, 64'h0100_0003, 0, 64'h4b80_0001, 1);
        check(0, 64'h8000_0000, 0, 64'hcf00_0000, 0);
        check(8, 64'h8000_0000, 0, 64'hc1e0_0000_0000_0000, 0);

        transfer = 4; // FTRC
        check(0, 64'hbfff_ffff, 0, 64'hffff_ffff, 0);
        check(0, 64'h7f80_0000, 0, 64'h7fff_ffff, 16);
        check(0, 64'hff80_0000, 0, 64'h8000_0000, 16);
        check(0, 64'h7fbf_ffff, 0, 64'h8000_0000, 16);
        check(0, 64'h0000_0001, 0, 0, 0);
        check(8, 64'hc1e0_0000_0000_0001, 0, 64'h8000_0000, 0);
        check(8, 64'hc1e0_0000_0020_0000, 0, 64'h8000_0000, 16);
        check(8, 64'h41e0_0000_0000_0000, 0, 64'h7fff_ffff, 16);

        transfer = 5; // FCNVSD
        check(8, 64'h3fc0_0000, 0, 64'h3ff8_0000_0000_0000, 0);
        check(8, 64'h8000_0000, 0, 64'h8000_0000_0000_0000, 0);
        check(8, 64'h7fc0_0000, 0, 64'h7ff7_ffff_ffff_ffff, 16);
        check(8, 64'h8000_0001, 0, 0, 32);
        check(9, 64'h8000_0001, 0, 64'h8000_0000_0000_0000, 0);

        transfer = 6; // FCNVDS
        check(8, 64'h3ff0_0000_1000_0000, 0, 64'h3f80_0000, 1);
        check(8, 64'h3ff0_0000_3000_0000, 0, 64'h3f80_0002, 1);
        check(10, 64'h3ff0_0000_3000_0000, 0, 64'h3f80_0001, 1);
        check(8, 64'h36a0_0000_0000_0000, 0, 64'h0000_0001, 0);
        check(9, 64'hb6a0_0000_0000_0000, 0, 64'h8000_0000, 3);
        check(8, 64'h7fef_ffff_ffff_ffff, 0, 64'h7f80_0000, 5);
        check(8, 64'h7ff7_ffff_ffff_ffff, 0, 64'h7fbf_ffff, 0);
        finished = 1;
    end
endmodule
