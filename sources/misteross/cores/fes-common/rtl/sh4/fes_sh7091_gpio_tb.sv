module fes_sh7091_gpio_tb (input logic clk, output logic finished);
    logic reset = 1, power_reset = 1, bus_tick = 1, port_enable = 0, request = 0;
    logic [31:2] address;
    logic [3:0] rmask, wstrb;
    logic [31:0] wdata, rdata;
    logic selected, rvalid;
    logic [19:0] pins_in = '1, pins_out, pins_oe, pins_pullup;
    logic irq;

    fes_sh7091_gpio dut (.clk, .reset, .power_reset, .bus_tick, .port_enable,
        .request, .address, .rmask, .wstrb, .wdata, .selected, .rvalid, .rdata,
        .pins_in, .pins_out, .pins_oe, .pins_pullup, .irq);

    task automatic write_reg(input logic [4:0] index, input logic [31:0] value,
                             input logic [3:0] lanes);
        @(negedge clk);
        address = {25'h1ff0000, index};
        wstrb = lanes;
        wdata = value;
        request = 1;
        @(negedge clk);
        assert (selected && !rvalid);
        request = 0;
    endtask

    task automatic read_reg(input logic [4:0] index, input logic [31:0] expected,
                            input logic [3:0] lanes);
        @(negedge clk);
        address = {25'h1ff0000, index};
        wstrb = 0;
        rmask = lanes;
        request = 1;
        @(negedge clk);
        assert (selected && rvalid && rdata == expected)
            else $fatal(1, "GPIO register %d returned %h, expected %h", index, rdata, expected);
        request = 0;
    endtask

    initial begin
        finished = 0;
        repeat (3) @(negedge clk);
        power_reset = 0;
        reset = 0;
        read_reg(11, 0, 4'hf);
        assert (pins_oe == 0 && pins_pullup == 0 && !irq);
        write_reg(12, 0, 4'hf); // Longword accesses to PDTRA are ignored.
        write_reg(12, 'haaaa, 4'h3);
        write_reg(17, 'hfffc, 4'h3);
        write_reg(11, 'h0000_004a, 4'hf); // A3 outputs, A1/A0 have no pull-ups.
        write_reg(16, 'hffff_ff01, 4'hf); // B0 outputs.
        assert (pins_out == 20'hcaaaa && !irq);
        port_enable = 1;
        #1;
        assert (pins_oe == 20'h10008 && pins_pullup == 20'hefff4);
        read_reg(16, 1, 4'hf);
        read_reg(17, 14, 4'h3);
        read_reg(12, 'hffff, 4'h3);
        write_reg(18, 11, 4'h3);
        bus_tick = 0;
        pins_in[3:0] = '0;
        repeat (4) @(negedge clk);
        assert (!irq);
        bus_tick = 1;
        @(negedge clk);
        assert (irq);
        read_reg(12, 'hfff8, 4'h3); // Output A3 retains its latch.
        write_reg(18, 8, 4'h3);
        assert (!irq);
        reset = 1;
        @(negedge clk);
        reset = 0;
        read_reg(11, 'h4a, 4'hf);
        read_reg(18, 8, 4'h3);
        power_reset = 1;
        reset = 1;
        @(negedge clk);
        power_reset = 0;
        reset = 0;
        read_reg(11, 0, 4'hf);
        read_reg(18, 0, 4'h3);
        assert (pins_oe == 0 && pins_out == 20'hcaaaa);
        finished = 1;
    end
endmodule
