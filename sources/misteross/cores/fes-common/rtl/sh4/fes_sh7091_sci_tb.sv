module fes_sh7091_sci_tb (input logic clk, output logic finished);
    logic reset = 1, stopped = 0, request = 0, external_sck = 1;
    logic [3:0] index, rmask, wstrb, irq;
    logic [7:0] wdata;
    logic rvalid, txd, txd_enable, sck_out, sck_enable;
    logic [31:0] rdata;
    logic [1:0] dma_request, dma_ready;

    fes_sh7091_sci dut (
        .clk, .reset, .peripheral_tick(1'b1), .stopped, .request, .dma_access(1'b0),
        .index, .rmask, .wstrb, .wdata, .rvalid, .rdata,
        .rxd(txd), .sck_in(external_sck), .txd, .txd_enable, .sck_out, .sck_enable,
        .irq, .dma_request, .dma_ready
    );

    task automatic write_reg(input logic [3:0] offset, input logic [7:0] value,
                             input logic [3:0] lanes = 4'h1);
        @(negedge clk);
        index = offset;
        wstrb = lanes;
        wdata = value;
        request = 1;
        @(negedge clk);
        assert (!rvalid);
        request = 0;
    endtask

    task automatic read_reg(input logic [3:0] offset, input logic [7:0] expected);
        @(negedge clk);
        index = offset;
        wstrb = 0;
        rmask = 4'h1;
        request = 1;
        @(negedge clk);
        assert (rvalid && rdata == {24'b0, expected})
            else $fatal(1, "SCI register %d returned %h, expected %h", offset, rdata, expected);
        request = 0;
    endtask

    initial begin
        finished = 0;
        repeat (3) @(negedge clk);
        reset = 0;
        read_reg(4, 'h84);
        write_reg(1, 0, 3);
        read_reg(1, 'hff);
        write_reg(1, 0);
        write_reg(0, 'h04); // Multiprocessor mode.
        write_reg(2, 'h78); // Enable reception of marked frames only.
        assert (txd_enable && !sck_enable && dma_ready == 2'b01 && dma_request == 2'b01);
        write_reg(3, 'h5a);
        write_reg(4, 'h7e); // Start an unmarked frame.
        repeat (400) @(negedge clk);
        assert (irq == 0 && !dma_request[1]);
        read_reg(2, 'h78);
        read_reg(4, 'h84);
        write_reg(3, 'ha7);
        write_reg(4, 'h7f); // Then send a marked frame.
        wait (irq[1]);
        repeat (40) @(negedge clk);
        assert (dma_request[1]);
        read_reg(2, 'h70);
        read_reg(5, 'ha7);
        read_reg(4, 'hc7);
        write_reg(4, 'hbf);
        assert (!irq[1]);

        write_reg(2, 0);
        write_reg(0, 'h80); // Synchronous internal clock.
        write_reg(1, 3);
        write_reg(3, 'h9d);
        write_reg(2, 'h20);
        read_reg(4, 'h87);
        write_reg(4, 'h7e);
        write_reg(2, 'h30);
        assert (sck_enable);
        wait (!sck_out);
        wait (dma_request[1]);
        write_reg(2, 0);
        read_reg(5, 'h9d);
        read_reg(4, 'hc4);
        write_reg(4, 'hbf);

        write_reg(2, 'h32); // Synchronous external clock.
        write_reg(3, 'h6b);
        read_reg(4, 'h85);
        write_reg(4, 'h7e);
        assert (!sck_enable);
        repeat (8) begin
            external_sck = 0;
            repeat (5) @(negedge clk);
            external_sck = 1;
            repeat (5) @(negedge clk);
        end
        read_reg(5, 'h6b);
        read_reg(4, 'hc4);
        stopped = 1;
        repeat (2) @(negedge clk);
        assert (txd_enable && !sck_enable);
        read_reg(2, 0);
        stopped = 0;
        read_reg(4, 'h84);
        read_reg(1, 'hff);
        finished = 1;
    end
endmodule
