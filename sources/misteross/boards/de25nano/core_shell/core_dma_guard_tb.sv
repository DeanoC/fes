// SPDX-License-Identifier: GPL-3.0-or-later
module core_dma_guard_tb;
    logic clk = 0;
    always #1 clk = ~clk;
    logic reset, stop, request_valid, request_ready, request_write;
    logic target_valid, target_ready, target_write, target_completed, fault, drained;
    logic [31:0] request_address, target_address;
    logic [15:0] request_bytes, target_bytes;
    fes_de25_core_dma_guard dut (.*);
    int forwarded = 0;
    string mode;

    always @(posedge clk) begin
        if (target_valid && target_ready) begin
            if (target_address < 32'h95000000 || target_address >= 32'h95800000 ||
                target_bytes == 0 || {1'b0, target_address}+{17'b0, target_bytes} > 33'h095800000)
                $fatal(1, "forwarded outside the window");
            forwarded++;
        end
    end

    task automatic step();
        @(negedge clk);
    endtask

    task automatic request(input logic [31:0] address, input logic [15:0] count);
        if (!request_ready) $fatal(1, "request blocked unexpectedly");
        request_address = address;
        request_bytes = count;
        request_write = 1;
        request_valid = 1;
        step();
        request_valid = 0;
    endtask

    task automatic reject(input logic [31:0] address, input logic [15:0] count);
        reset = 1;
        step();
        reset = 0;
        step();
        request(address, count);
        repeat (4) step();
        if (!fault || !drained || request_ready || forwarded != 1)
            $fatal(1, "invalid request was not contained");
    endtask

    initial begin
        stop = 0;
        target_ready = 0;
        target_completed = 0;
        request_valid = 0;
        request_address = 0;
        request_bytes = 0;
        request_write = 0;
        if (!$value$plusargs("mode=%s", mode)) $fatal(1, "mode required");
        reset = 1;
        step();
        reset = 0;
        step();
        if (mode == "bad-dma") begin
            target_ready = 1;
            request(32'h957ffffc, 4);
            if (!target_valid || !target_write || target_address != 32'h957ffffc)
                $fatal(1, "last legal word not staged");
            step();
            if (request_ready || drained || forwarded != 1)
                $fatal(1, "outstanding work was lost");
            target_completed = 1;
            step();
            target_completed = 0;
            request(32'h957ffffc, 8);
            repeat (4) step();
            if (!fault || !drained || request_ready || forwarded != 1)
                $fatal(1, "bad request was not contained");
            reject(32'h94fffffc, 4);
            reject(32'h95000000, 0);
            reject(32'hfffffffc, 8);
            $display("fault=1 drained=1 forwarded=1");
        end else if (mode == "wedged") begin
            request(32'h95000000, 4);
            stop = 1;
            repeat (4) step();
            if (!target_valid || target_address != 32'h95000000 || drained)
                $fatal(1, "stopping dropped a stalled request");
            target_ready = 1;
            repeat (16) step();
            if (request_ready || drained || fault || forwarded != 1)
                $fatal(1, "missing completion was treated as drain");
            $display("fault=0 drained=0 forwarded=1");
        end else begin
            $fatal(1, "unknown mode");
        end
        $finish;
    end
endmodule
