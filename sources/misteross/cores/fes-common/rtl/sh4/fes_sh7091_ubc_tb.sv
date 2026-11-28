module fes_sh7091_ubc_tb (input logic clk, output logic finished);
    logic reset = 1, power_reset = 1, request = 0, observed = 0;
    logic [31:2] address;
    logic [3:0] rmask, wstrb;
    logic [31:0] wdata, rdata, observed_address;
    logic selected, rvalid, operand, written, data_present;
    logic [2:0] operand_size;
    logic [7:0] observed_asid;
    logic [63:0] observed_data;
    logic enabled, pre_break, post_break, debug_vector;
    logic [1:0] post_matches, retired_matches = 0;

    fes_sh7091_ubc dut (.allow_pre(1'b1), .allow_post(1'b1), .*);

    task automatic write_reg(input logic [31:0] addr, value, input logic [3:0] lanes);
        @(negedge clk);
        assert (addr[1:0] == 0);
        address = addr[31:2];
        wstrb = lanes;
        wdata = value;
        request = 1;
        @(negedge clk);
        assert (selected && !rvalid);
        request = 0;
    endtask

    task automatic read_reg(input logic [31:0] addr, expected, input logic [3:0] lanes);
        @(negedge clk);
        assert (addr[1:0] == 0);
        address = addr[31:2];
        wstrb = 0;
        rmask = lanes;
        request = 1;
        @(negedge clk);
        assert (selected && rvalid && rdata == expected)
            else $fatal(1, "UBC register %h returned %h, expected %h", addr, rdata, expected);
        request = 0;
    endtask

    task automatic observe(input logic [31:0] addr, input logic [7:0] asid,
                           input logic data_cycle, store, data_valid, input logic [2:0] size,
                           input logic [63:0] value, input logic want_pre, want_post, commit_hit = 1'b1);
        @(negedge clk);
        observed = 1;
        observed_address = addr;
        observed_asid = asid;
        operand = data_cycle;
        written = store;
        data_present = data_valid;
        operand_size = size;
        observed_data = value;
        #1;
        assert (pre_break == want_pre && !post_break)
            else $fatal(1, "Incorrect pre-execution break at %h", addr);
        @(negedge clk);
        retired_matches = commit_hit ? post_matches : 2'b0;
        observed = 0;
        #1;
        assert (post_break == want_post)
            else $fatal(1, "Incorrect retirement break at %h", addr);
        @(negedge clk);
        retired_matches = 0;
    endtask

    initial begin
        finished = 0;
        repeat (2) @(negedge clk);
        reset = 0;
        power_reset = 0;
        assert (!enabled && !debug_vector);
        write_reg('hff20_0000, 'h1234_5670, 4'hf);
        write_reg('hff20_0004, 0, 4'h1);
        write_reg('hff00_0014, 'h17, 4'h1);
        write_reg('hff20_0008, 'h16, 4'h3); // Fetch, read, word.
        assert (enabled);
        observe('h1234_5670, 'h18, 0, 0, 1, 2, 0, 0, 0);
        observe('h1234_5670, 'h17, 0, 0, 1, 2, 0, 1, 0);
        read_reg('hff20_0020, 'h8000, 4'h3);
        write_reg('hff20_0020, 'h0401, 4'h3); // Post-execution and DBR.
        assert (debug_vector);
        observe('h1234_5670, 'h17, 0, 0, 1, 2, 0, 0, 0, 0);
        read_reg('hff20_0020, 'h0401, 4'h3); // Cancelled operation did not set CMFA.
        observe('h1234_5670, 'h17, 0, 0, 1, 2, 0, 0, 1);
        read_reg('hff20_0020, 'h8401, 4'h3);

        write_reg('hff20_0020, 'h0088, 4'h3); // A arms B, B includes data.
        write_reg('hff20_0004, 'h05, 4'h1); // Ignore ASID and low 10 bits.
        write_reg('hff20_000c, 'h0c00_0180, 4'hf);
        write_reg('hff20_0010, 'h04, 4'h1);
        write_reg('hff20_0014, 'h68, 4'h3); // Operand, write, quadword.
        write_reg('hff20_0018, 'h1122_3344, 4'hf);
        write_reg('hff20_001c, 'h0000_00ff, 4'hf);
        observe('h0c00_0184, 0, 1, 1, 1, 4, 'h1122_3300_5566_7788, 0, 0);
        observe('h1234_5550, 0, 0, 0, 0, 2, 0, 0, 0);
        observe('h0c00_0184, 0, 1, 1, 0, 4, 'h1122_3300_5566_7788, 0, 0);
        observe('h0c00_0184, 0, 1, 1, 1, 4, 'h1122_3300_5566_7788, 0, 1);
        observe('h0c00_0184, 0, 1, 1, 1, 4, 'h1122_3300_5566_7788, 0, 0);
        read_reg('hff20_0020, 'h4088, 4'h3);
        finished = 1;
    end
endmodule
