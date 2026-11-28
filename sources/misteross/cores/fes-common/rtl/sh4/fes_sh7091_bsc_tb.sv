module fes_sh7091_bsc_tb(input logic clk, output logic finished);
    logic reset, bus_tick, request;
    logic [31:2] address;
    logic [31:0] wdata, rdata;
    logic [3:0] rmask, wstrb;
    logic selected, rvalid;
    logic [1:0] refresh_irq, sdram_command;
    logic sdram_mode_set;
    logic [11:0] sdram_mode_address;

    fes_sh7091_bsc dut (.*);

    task automatic write_reg(input logic [31:0] addr, value, input logic [3:0] mask);
        @(negedge clk);
        assert (addr[1:0] == 0) else $fatal(1, "BSC test bus address must be word-aligned");
        address = addr[31:2];
        wdata = value;
        wstrb = mask;
        request = 1;
        @(negedge clk);
        assert (selected && !rvalid) else $fatal(1, "BSC write at %h was not decoded", addr);
        request = 0;
    endtask

    task automatic read_reg(input logic [31:0] addr, expected, input logic [3:0] mask);
        @(negedge clk);
        assert (addr[1:0] == 0) else $fatal(1, "BSC test bus address must be word-aligned");
        address = addr[31:2];
        rmask = mask;
        wstrb = 0;
        request = 1;
        @(negedge clk);
        assert (selected && rvalid && rdata == expected)
            else $fatal(1, "BSC read %h returned %h, expected %h", addr, rdata, expected);
        request = 0;
    endtask

    initial begin
        reset = 1;
        request = 0;
        bus_tick = 0;
        finished = 0;
        @(negedge clk);
        reset = 0;
        read_reg(32'hff80_0000, 32'hc000_0000, 4'hf);
        read_reg(32'hff80_0004, 32'h0000_bffc, 4'h3);
        read_reg(32'hff80_0008, 32'h7777_7777, 4'hf);
        write_reg(32'hff80_0004, 1, 4'h3);
        write_reg(32'hff80_0004, '1, 4'h1);
        read_reg(32'hff80_0004, 32'h8001, 4'h3);
        write_reg(32'hff80_0000, '1, 4'hf);
        read_reg(32'hff80_0000, 32'hc33e_fffd, 4'hf);
        write_reg(32'hff80_0010, '1, 4'hf);
        read_reg(32'hff80_0010, 32'h0777_7777, 4'hf);

        write_reg(32'hff90_0010, 0, 4'h1);
        assert (sdram_command == 1 && !sdram_mode_set && sdram_mode_address == 12'h010)
            else $fatal(1, "SDRAM area 2 precharge command was incorrect");
        write_reg(32'hff80_0014, 32'h4000_0000, 4'hf);
        write_reg(32'hff94_0040, 0, 4'h4);
        assert (sdram_command == 2 && sdram_mode_set && sdram_mode_address == 12'h042)
            else $fatal(1, "SDRAM area 3 mode command lost the byte address");

        write_reg(32'hff80_0024, 32'h0000_5a02, 4'h3);
        read_reg(32'hff80_0024, 0, 4'h3);
        write_reg(32'hff80_0024, 32'h0000_a502, 4'h3);
        write_reg(32'hff80_0028, 32'h0000_a5ff, 4'h3);
        write_reg(32'hff80_001c, 32'h0000_a54b, 4'h3);
        bus_tick = 1;
        repeat (7) @(negedge clk);
        assert (refresh_irq == 0) else $fatal(1, "BSC refresh interrupt was early");
        @(negedge clk);
        bus_tick = 0;
        assert (refresh_irq == 2'b11) else $fatal(1, "BSC compare/overflow interrupts were missing");
        read_reg(32'hff80_0020, 0, 4'h3);
        read_reg(32'hff80_0028, 0, 4'h3);
        read_reg(32'hff80_001c, 32'hcf, 4'h3);
        write_reg(32'hff80_001c, 32'h0000_a5cf, 4'h3);
        assert (refresh_irq == 2'b11) else $fatal(1, "Writing 1 must preserve BSC timer flags");
        write_reg(32'hff80_001c, 32'h0000_a54b, 4'h3);
        assert (refresh_irq == 0) else $fatal(1, "Writing 0 must clear BSC timer flags");
        finished = 1;
    end
endmodule
