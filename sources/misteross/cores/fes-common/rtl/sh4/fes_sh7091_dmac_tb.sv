// Exercise DMA and CPU contention at the shared external memory port.
module fes_sh7091_dmac_tb (input logic clk, output logic finished);
    logic ddt_valid = 0, ddt_ready;
    logic [1:0] ddt_kind = 0, device_channel;
    logic [63:0] ddt_command = 0;
    logic device_valid, device_write, device_last, device_ready = 0;
    logic [3:0] device_mask;
    logic [31:0] device_wdata, device_rdata = 0;
    logic reset = 1, nmi = 0;
    logic reg_request = 0;
    logic [31:2] reg_addr;
    logic [3:0] reg_rmask, reg_wstrb;
    logic [31:0] reg_wdata, reg_rdata;
    logic reg_selected, reg_rvalid;
    logic [1:0] dreq_n = 3, dreq_ack;
    logic capture_pending = 0, capture_accept;
    logic [4:0] irq;
    logic pause = 0, stopped = 0;
    logic [1:0] unused_dma_serial;
    logic dma_valid, dma_ready, dma_burst, dma_rvalid;
    logic [31:0] dma_addr, dma_wdata;
    logic [3:0] dma_rmask, dma_wstrb;
    logic cpu_valid = 0, cpu_lock = 0, cpu_sq = 0, cpu_ready, cpu_rvalid;
    logic [31:0] cpu_addr;
    logic [3:0] cpu_wstrb;
    logic mem_valid, mem_lock, mem_sq, mem_rvalid = 0, mem_ready = 1;
    logic [31:0] mem_addr, mem_wdata, mem_rdata;
    logic [3:0] mem_rmask, mem_wstrb;
    logic [31:0] memory [0:127];

    logic unused_idle;
    fes_sh7091_dmac dma (
        .ddt_valid, .ddt_kind, .ddt_command, .ddt_ready,
        .device_valid, .device_write, .device_last, .device_channel, .device_mask,
        .device_wdata, .device_ready, .device_rdata,
        .idle(unused_idle),
        .clk, .reset, .nmi, .pause, .stopped, .reg_request, .reg_addr, .reg_rmask, .reg_wstrb,
        .reg_wdata, .reg_selected, .reg_rvalid, .reg_rdata, .dreq_n, .dreq_ack,
        .capture_pending, .capture_value(32'h1234_5678), .capture_accept, .irq,
        .serial_request(4'b0), .mem_serial(unused_dma_serial), .mem_valid(dma_valid), .mem_burst(dma_burst), .mem_ready(dma_ready),
        .mem_addr(dma_addr), .mem_wdata(dma_wdata), .mem_rmask(dma_rmask),
        .mem_wstrb(dma_wstrb), .mem_rvalid(dma_rvalid), .mem_rdata
    );
    fes_sh7091_bus arbiter (
        .clk, .reset, .cpu_valid, .cpu_lock, .cpu_sq, .cpu_addr, .cpu_wdata(32'hcafe_cafe),
        .cpu_rmask(4'hf), .cpu_wstrb, .cpu_ready, .cpu_rvalid, .dma_valid, .dma_burst,
        .dma_addr, .dma_wdata, .dma_rmask, .dma_wstrb, .dma_ready, .dma_rvalid,
        .mem_valid, .mem_lock, .mem_sq, .mem_addr, .mem_wdata, .mem_rmask,
        .mem_wstrb, .mem_ready, .mem_rvalid
    );

    always @(posedge clk) begin
        mem_rvalid <= mem_valid && mem_ready && mem_wstrb == 0;
        if (mem_valid && mem_ready) begin
            if (mem_wstrb == 0) begin
                assert (mem_rmask != 0);
                mem_rdata <= memory[mem_addr[8:2]];
            end else for (int lane = 0; lane < 4; lane++)
                if (mem_wstrb[lane])
                    memory[mem_addr[8:2]][lane*8 +: 8] <= mem_wdata[lane*8 +: 8];
            assert (!(dma_ready && (mem_lock || mem_sq)));
            assert (!dreq_ack[0]);
        end
    end

    task automatic write_reg(input logic [6:0] offset, input logic [31:0] value);
        @(negedge clk);
        assert (offset[1:0] == 0);
        reg_addr = {25'h1ff4000, offset[6:2]};
        reg_request = 1;
        reg_rmask = 0;
        reg_wstrb = 4'hf;
        reg_wdata = value;
        #1;
        assert (reg_selected);
        @(negedge clk);
        reg_request = 0;
    endtask

    task automatic check_reg(input logic [6:0] offset, input logic [31:0] expected);
        @(negedge clk);
        assert (offset[1:0] == 0);
        reg_addr = {25'h1ff4000, offset[6:2]};
        reg_request = 1;
        reg_rmask = 4'hf;
        reg_wstrb = 0;
        @(negedge clk);
        assert (reg_rvalid && reg_rdata == expected)
            else $fatal(1, "DMAC register %h gave %h, expected %h", offset, reg_rdata, expected);
        reg_request = 0;
    endtask

    task automatic channel(input logic [1:0] index, input logic [31:0] source, dest, count, mode);
        write_reg({1'b0, index, 4'h0}, source);
        write_reg({1'b0, index, 4'h4}, dest);
        write_reg({1'b0, index, 4'h8}, count);
        write_reg({1'b0, index, 4'hc}, mode);
    endtask

    task automatic cpu_write(input logic [31:0] address);
        cpu_valid = 1;
        cpu_addr = address;
        cpu_wstrb = 4'hf;
        do @(posedge clk); while (!cpu_ready);
        assert (!dma_ready);
        @(negedge clk);
        cpu_valid = 0;
    endtask

    task automatic demand_request(input logic [1:0] kind, input logic [63:0] command);
        @(negedge clk);
        ddt_kind = kind;
        ddt_command = command;
        ddt_valid = 1;
        #1;
        assert (ddt_ready) else $fatal(1, "Unexpected DDT backpressure, command %h", command);
        @(negedge clk);
        ddt_valid = 0;
    endtask

    task automatic check_device_write(input logic [1:0] channel_id, input logic [3:0] mask,
                                      input logic [31:0] value, input logic last);
        wait (device_valid);
        repeat (3) begin
            @(negedge clk);
            assert (device_valid && device_write && device_channel == channel_id &&
                    device_mask == mask && device_wdata == value && device_last == last)
                else $fatal(1, "Unexpected DMA device beat on channel %d: %h", device_channel, device_wdata);
        end
        device_ready = 1;
        @(negedge clk);
        device_ready = 0;
    endtask

    initial begin
        finished = 0;
        repeat (3) @(negedge clk);
        reset = 0;
        for (int i = 0; i < 16; i++)
            memory[16+i] = 32'h1234_0100 + 32'(i);
        channel(0, 'h40, 'h80, 2, 'h5445);
        mem_ready = 0;
        pause = 1;
        write_reg('h40, 1);
        repeat (6) @(negedge clk);
        assert (!dma_valid); // Deep sleep leaves the configured request pending.
        stopped = 1;
        check_reg('h08, 0);
        write_reg('h08, 1);
        stopped = 0;
        check_reg('h08, 2);
        pause = 0;
        wait (mem_valid);
        @(negedge clk);
        cpu_valid = 1;
        cpu_addr = 'h1c0;
        cpu_wstrb = 0;
        repeat (4) begin
            @(negedge clk);
            assert (mem_valid && mem_addr == 'h40 && mem_wstrb == 0 && !cpu_ready)
                else $fatal(1, "DMA request changed under backpressure");
        end
        mem_ready = 1;
        do @(posedge clk); while (!cpu_ready);
        @(negedge clk);
        cpu_valid = 0;
        wait (cpu_rvalid);
        wait (irq[0]);
        for (int i = 0; i < 16; i++)
            assert (memory[32+i] == 32'h1234_0100 + 32'(i))
                else $fatal(1, "DMA data mismatch at word %0d", i);
        check_reg('h00, 'h80);
        check_reg('h04, 'hc0);
        check_reg('h08, 0);
        write_reg('h0c, 'h5444);
        assert (irq[0]);
        check_reg('h0c, 'h5446);
        write_reg('h0c, 'h5444);
        assert (!irq[0]);

        memory['h40] = 'h4433_2211;
        memory['h48] = 'hffcc_aa99;
        cpu_sq = 1;
        cpu_write('h180);
        channel(2, 'h101, 'h123, 1, 'h415);
        wait (dma_valid);
        @(negedge clk);
        repeat (3) begin
            @(negedge clk);
            assert (!mem_valid) else $fatal(1, "DMA interrupted an open store queue");
        end
        for (int i = 1; i < 8; i++)
            cpu_write(32'h180 + (32'(i) << 2));
        cpu_sq = 0;
        wait (irq[2]);
        assert (memory['h48] == 'h22cc_aa99);
        check_reg('h2c, 'h417);
        write_reg('h2c, 'h414);
        channel(2, 'h101, 'h124, 1, 'h425);
        wait (irq[4]);
        assert (!dma_valid);
        write_reg('h40, 1);
        assert (irq[4]);
        check_reg('h40, 5);
        write_reg('h2c, 'h424);
        write_reg('h40, 1);
        assert (!irq[4]);

        nmi = 1;
        @(negedge clk);
        nmi = 0;
        check_reg('h40, 3);
        write_reg('h40, 1);
        channel(3, 'hffd8_002c, 'h140, 1, 'he35);
        capture_pending = 1;
        wait (capture_accept);
        @(negedge clk);
        capture_pending = 0;
        wait (irq[3]);
        assert (memory['h50] == 'h1234_5678);

        channel(1, 'h100, 'h148, 1, 'h80035);
        cpu_lock = 1;
        cpu_valid = 1;
        cpu_addr = 'h1c0;
        cpu_wstrb = 0;
        do @(posedge clk); while (!cpu_ready);
        @(negedge clk);
        cpu_valid = 0;
        assert (cpu_rvalid && mem_lock);
        dreq_n[1] = 0;
        wait (dreq_ack[1]);
        @(negedge clk);
        repeat (3) begin
            @(negedge clk);
            assert (!mem_valid && mem_lock)
                else $fatal(1, "DMA interrupted a locked read-modify-write");
        end
        cpu_write('h1c0);
        cpu_lock = 0;
        wait (irq[1]);
        assert (memory['h52] == 'h4433_2211);
        // A normal single-address burst drains the programmed count
        // after one falling DREQ edge.
        channel(0, 'h40, 0, 2, 'h812b5);
        @(negedge clk);
        dreq_n[0] = 0;
        wait (dreq_ack[0]);
        @(negedge clk);
        dreq_n[0] = 1;
        check_device_write(0, 'hf, 'h1234_0100, 1);
        check_device_write(0, 'hf, 'h1234_0101, 1);
        wait (irq[0]);

        // Demand channel 0 takes its direction, size and count from DTR,
        // and masks writes from the CPU while DMAOR.DDT is set.
        write_reg('h40, 'h8201);
        memory['h60] = 'hfedc_ba98;
        demand_request(0, {3'd0, 1'b0, 2'd0, 2'd3, 8'd2, 16'hffff, 32'h181});
        check_device_write(0, 1, 'h00fe_dcba, 1);
        wait (unused_idle);
        repeat (3) @(negedge clk);
        assert (!device_valid);
        check_reg('h00, 'h182);
        check_reg('h08, 1);
        write_reg('h00, 0);
        check_reg('h00, 'h182);
        demand_request(1, 0);
        check_device_write(0, 1, 'h0000_fedc, 1);
        wait (unused_idle);
        check_reg('h0c, 'h5213);

        // Quadword DTR uses a different size encoding from CHCR.
        demand_request(0, {3'd3, 1'b1, 2'd0, 2'd1, 8'd1, 16'b0, 32'h190});
        for (int i = 0; i < 2; i++) begin
            wait (device_valid);
            @(negedge clk);
            assert (device_valid && !device_write && device_channel == 0 &&
                    device_mask == 'hf && device_last == (i == 1));
            device_rdata = 32'h1290_2000 + 32'(i);
            device_ready = 1;
            @(negedge clk);
            device_ready = 0;
        end
        wait (unused_idle);
        assert (memory['h64] == 'h1290_2000 && memory['h65] == 'h1290_2001);
        check_reg('h04, 'h198);
        check_reg('h08, 0);

        // A fixed-length burst is continued internally after its first request.
        demand_request(0, {3'd2, 1'b0, 2'd0, 2'd1, 8'd2, 16'b0, 32'h190});
        check_device_write(0, 'hf, 'h1290_2000, 1);
        check_device_write(0, 'hf, 'h1290_2001, 1);
        wait (unused_idle);

        // COUNT=0 is the maximum. A stop command after one requested unit
        // retains the residue and clears DE without setting TE.
        demand_request(0, {3'd2, 1'b0, 2'd0, 2'd2, 8'd0, 16'b0, 32'h190});
        check_device_write(0, 'hf, 'h1290_2000, 1);
        wait (unused_idle);
        demand_request(0, {3'd7, 1'b0, 2'd0, 2'd2, 8'd0, 16'b0, 32'b0});
        check_reg('h08, 'hff_ffff);
        check_reg('h0c, 'h52b0);

        // Queue four channel-3 requests with the CPU's channel registers.
        check_reg('h3c, 'he37);
        write_reg('h3c, 0);
        channel(3, 'h40, 'h1a0, 4, 'h5035);
        pause = 1;
        demand_request(0, {4'b0, 2'd3, 58'b0});
        repeat (3) demand_request(1, 0);
        #1;
        assert (!ddt_ready);
        pause = 0;
        wait (irq[3]);
        for (int i = 0; i < 4; i++)
            assert (memory['h68+i] == 32'h1234_0100 + 32'(i));
        finished = 1;
    end
endmodule
