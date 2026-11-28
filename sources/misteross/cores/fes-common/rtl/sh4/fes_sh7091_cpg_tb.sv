module fes_sh7091_cpg_tb (input logic clk, output logic finished);
    logic reset = 1, power_reset = 1, watchdog_reset = 0, request = 0;
    logic sleeping = 0, selected, rvalid, frequency_change;
    logic standby, deep_sleep, peripheral_high_z, peripheral_pullup;
    logic [31:2] address;
    logic [3:0] rmask, wstrb;
    logic [11:0] wdata, frequency_control;
    logic [31:0] rdata;
    logic [4:0] module_stop;
    logic [1:0] processor_status;

    fes_sh7091_cpg dut (.*);
    task automatic write_reg(input logic [2:0] index, input logic [11:0] value, input logic [3:0] lanes);
        @(negedge clk);
        address = {27'h7fe0000, index};
        wstrb = lanes;
        wdata = value;
        request = 1;
        @(negedge clk);
        assert (selected && !rvalid);
        request = 0;
    endtask

    task automatic read_reg(input logic [2:0] index, input logic [31:0] expected, input logic [3:0] lanes);
        @(negedge clk);
        address = {27'h7fe0000, index};
        wstrb = 0;
        rmask = lanes;
        request = 1;
        @(negedge clk);
        assert (selected && rvalid && rdata == expected);
        request = 0;
    endtask

    initial begin
        finished = 0;
        repeat (2) @(negedge clk);
        assert (processor_status == 3);
        reset = 0;
        power_reset = 0;
        read_reg(0, 'he0a, 3);
        write_reg(0, 'ha09, 1);
        assert (frequency_control == 'he0a && !frequency_change);
        write_reg(0, 'ha09, 3);
        assert (frequency_control == 'ha09 && frequency_change);
        write_reg(1, 'h0d, 1);
        assert (module_stop == 'h0d && !frequency_change);
        sleeping = 1;
        #1;
        assert (processor_status == 2 && !standby && !deep_sleep && !peripheral_high_z && peripheral_pullup);
        write_reg(4, 'hfff, 1);
        read_reg(4, 'h80, 1);
        assert (deep_sleep && !standby);
        write_reg(1, 'he0, 1);
        assert (!deep_sleep && standby && peripheral_high_z && !peripheral_pullup && processor_status == 1);
        reset = 1;
        watchdog_reset = 1;
        @(negedge clk);
        reset = 0;
        watchdog_reset = 0;
        read_reg(1, 0, 1);
        read_reg(4, 0, 1);
        read_reg(0, 'ha09, 3);
        assert (!standby && !deep_sleep);
        finished = 1;
    end
endmodule
