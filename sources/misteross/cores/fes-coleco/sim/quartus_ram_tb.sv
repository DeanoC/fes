// SPDX-License-Identifier: GPL-2.0-or-later
`timescale 1ns/1ps
// Run with QUARTUS and Intel's unmodified altera_mf.v, not a RAM stand-in.
module quartus_ram_tb;
    reg clock = 0;
    always #10 clock = !clock;
    reg [3:0] address = 0;
    reg [7:0] data = 0;
    reg write = 0;
    wire [7:0] qa, qb, video_q;
    coleco_dpram #(.ADDRWIDTH(4), .NUMWORDS(16)) ram (
        .clock(clock), .address_a(address), .data_a(data), .wren_a(write), .q_a(qa),
        .address_b(address), .data_b(8'b0), .wren_b(1'b0), .q_b(qb));
    coleco_video_dpram #(.DATAWIDTH(8), .ADDRWIDTH(4), .NUMWORDS(16)) video (
        .clock_a(clock), .address_a(address), .data_a(data), .wren_a(write),
        .clock_b(clock), .address_b(address), .q_b(video_q));
    integer i;
    initial begin
        // Initialize through the production write ports, avoid mixed-port collisions.
        for (i = 0; i < 4; i = i + 1) begin
            @(negedge clock); address = i; data = 8'h31 + i; write = 1;
        end
        @(negedge clock); write = 0; address = 0;
        repeat (3) @(negedge clock);
        for (i = 1; i < 4; i = i + 1) begin
            address = i;
            #1;
            if (qa !== 8'h30 + i || qb !== 8'h30 + i)
                $fatal(1, "Quartus RAM address must stay registered before clock");
            @(posedge clock); #1;
            $display("address %0d: RAM A=%h B=%h framebuffer=%h expected=%h",
                     i, qa, qb, video_q, 8'h31 + i);
            if (qa !== 8'h31 + i || qb !== 8'h31 + i)
                $fatal(1, "Quartus RAM one-edge read mismatch");
            if (video_q !== 8'h31 + i)
                $fatal(1, "Quartus framebuffer adds an unexpected read cycle");
            @(negedge clock);
        end
        $display("Quartus vendor RAM address/output latency passed");
        $finish;
    end
    initial begin #10000; $fatal(1, "RAM test timeout"); end
endmodule
