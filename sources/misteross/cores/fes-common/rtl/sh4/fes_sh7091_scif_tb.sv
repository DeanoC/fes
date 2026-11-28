module fes_sh7091_scif_tb (input logic clk, output logic finished);
    logic reset = 1, request = 0;
    logic [3:0] index;
    logic [3:0] rmask, wstrb;
    logic [7:0] wdata;
    logic rvalid, txd, txd_enable, rts_n;
    logic [31:0] rdata;
    logic [3:0] irq;
    logic [1:0] dma_request, dma_ready;

    fes_sh7091_scif dut (.clk, .reset, .peripheral_tick(1'b1), .stopped(1'b0), .external_tick(1'b0),
        .request, .dma_access(1'b0), .index, .rmask, .wstrb, .wdata,
        .rvalid, .rdata, .rxd(1'b1), .cts_n(1'b0), .txd, .txd_enable, .rts_n,
        .irq, .dma_request, .dma_ready);

    task automatic write_reg(input logic [3:0] offset, input logic [7:0] value,
                             input logic [3:0] lanes);
        @(negedge clk);
        index = offset;
        wstrb = lanes;
        wdata = value;
        request = 1;
        @(negedge clk);
        assert (!rvalid);
        request = 0;
    endtask

    task automatic read_reg(input logic [3:0] offset, input logic [31:0] expected,
                            input logic [3:0] lanes);
        @(negedge clk);
        index = offset;
        wstrb = 0;
        rmask = lanes;
        request = 1;
        @(negedge clk);
        assert (rvalid && rdata == expected)
            else $fatal(1, "SCIF register %d returned %h, expected %h", offset, rdata, expected);
        request = 0;
    endtask

    initial begin
        finished = 0;
        repeat (3) @(negedge clk);
        reset = 0;
        read_reg(1, 'hff, 1);
        read_reg(4, 'h60, 3);
        write_reg(1, 0, 1);
        write_reg(6, 1, 3);
        write_reg(2, 'h70, 3);
        assert (txd_enable && txd && !rts_n && dma_request[0] && !irq[3]);
        write_reg(3, 'h93, 1);
        write_reg(3, 'h48, 1);
        wait (irq[1]);
        assert (dma_request[1] && dma_ready == 2'b11);
        repeat (355) @(negedge clk);
        read_reg(7, 2, 3);
        read_reg(5, 'h93, 1);
        read_reg(5, 'h48, 1);
        read_reg(7, 0, 3);
        read_reg(4, 'h62, 3);
        write_reg(4, 'hfd, 3);
        assert (!(|irq) && !dma_request[1]);
        assert (dma_ready == 2'b01);
        finished = 1;
    end
endmodule
