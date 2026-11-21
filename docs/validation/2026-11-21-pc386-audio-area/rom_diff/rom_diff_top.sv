module rom_diff_top (input wire clk, input wire [7:0] a, b,
                     output wire [11:0] ls_old, ls_new, output wire [9:0] ex_old, ex_new);
    old_log_sine o1 (.clk(clk), .theta(a), .out(ls_old));
    old_exp o2 (.clk(clk), .in(b), .out(ex_old));
    opl2_phase_rom n (.clk(clk), .theta(a), .exp_in(b), .log_sin(ls_new), .exp_out(ex_new));
endmodule
