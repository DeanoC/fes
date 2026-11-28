module fes_sh7091_rtc_tb (input logic clk, output logic finished);
    logic reset = 1, power_reset = 1, tick = 0, request = 0;
    logic [31:2] address;
    logic [3:0] rmask, wstrb;
    logic [15:0] wdata;
    logic selected, rvalid, tmu_tick, clock_out;
    logic [31:0] rdata;
    logic [2:0] irq;

    fes_sh7091_rtc rtc (.clk, .reset, .power_reset, .tick, .request, .address,
        .rmask, .wstrb, .wdata, .selected, .rvalid, .rdata, .irq, .tmu_tick, .clock_out);

    task automatic write_reg(input logic [3:0] index, input logic [15:0] value,
                             input logic [3:0] strobes = 4'h1);
        @(negedge clk);
        address = {26'h3ff2000, index};
        wstrb = strobes;
        wdata = value;
        request = 1;
        @(negedge clk);
        assert (selected && !rvalid);
        request = 0;
    endtask

    task automatic read_reg(input logic [3:0] index, input logic [31:0] expected,
                            input logic [3:0] lanes = 4'h1);
        @(negedge clk);
        address = {26'h3ff2000, index};
        wstrb = 0;
        rmask = lanes;
        request = 1;
        @(negedge clk);
        assert (selected && rvalid && rdata == expected)
            else $fatal(1, "RTC register %d returned %h, expected %h", index, rdata, expected);
        request = 0;
    endtask

    task automatic advance(input int clocks);
        tick = 1;
        repeat (clocks) @(negedge clk);
        tick = 0;
    endtask

    task automatic set_calendar(input logic [15:0] year,
                               input logic [7:0] month, day);
        write_reg(15, 16'h000a); // Stop calendar and reset the divider.
        write_reg(1, 16'h0059);
        write_reg(2, 16'h0059);
        write_reg(3, 16'h0023);
        write_reg(4, 16'h0006);
        write_reg(5, {8'b0, day});
        write_reg(6, {8'b0, month});
        write_reg(7, year, 4'h3);
    endtask

    initial begin
        finished = 0;
        repeat (2) @(negedge clk);
        reset = 0;
        power_reset = 0;
        read_reg(15, 'h09);
        set_calendar(16'h1999, 8'h12, 8'h31);
        write_reg(8, 16'h0080); // Midnight alarm, second field.
        write_reg(10, 16'h0080); // Also require hour zero.
        write_reg(14, 16'h0018);
        write_reg(15, 16'h0019); // Enable 1/256 s IRQ and start calendar.
        advance(127);
        #1;
        assert (clock_out && !tmu_tick);
        assert (irq == 0);
        advance(1);
        assert (irq == 3'b010 && !clock_out);
        read_reg(0, 0);
        advance(128);
        read_reg(0, 1);
        advance(32768 - 256);
        @(negedge clk);
        assert (irq == 3'b111);
        read_reg(1, 0);
        read_reg(2, 0);
        read_reg(3, 0);
        read_reg(4, 0);
        read_reg(5, 1);
        read_reg(6, 1);
        read_reg(7, 'h2000, 4'h3);
        read_reg(7, 0); // Year is a 16-bit register.
        write_reg(14, 16'h0018);
        assert (!irq[2]);
        @(negedge clk);
        assert (irq[0]); // The matching alarm reasserts.
        write_reg(8, 0);
        write_reg(10, 0);
        write_reg(14, 0);
        write_reg(15, 16'h0009);
        assert (irq == 0);

        set_calendar(16'h2000, 8'h02, 8'h28);
        write_reg(15, 16'h0009);
        advance(32768);
        read_reg(5, 'h29);
        read_reg(6, 2);
        set_calendar(16'h2100, 8'h02, 8'h28);
        write_reg(15, 16'h0009);
        advance(32768);
        read_reg(5, 1);
        read_reg(6, 3);

        write_reg(15, 16'h000a);
        write_reg(1, 16'h0030);
        write_reg(2, 16'h0059);
        write_reg(3, 16'h0023);
        write_reg(15, 16'h000c); // Round to the next day, while stopped.
        read_reg(1, 0);
        read_reg(2, 0);
        read_reg(3, 0);
        read_reg(5, 2);
        read_reg(15, 8);
        advance(512);
        read_reg(0, 2);
        reset = 1;
        advance(256);
        reset = 0;
        read_reg(5, 2);
        read_reg(0, 3);
        read_reg(15, 8);
        finished = 1;
    end
endmodule
