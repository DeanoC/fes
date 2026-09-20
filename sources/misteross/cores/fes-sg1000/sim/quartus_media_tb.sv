// SPDX-License-Identifier: GPL-2.0-or-later
`timescale 1ns/1ps
module quartus_media_tb;
    reg clk = 0;
    always #10 clk = !clk;
    reg [31:0] gpo = 0;
    wire [31:0] gpi;
    wire reset, ready;
    wire [39:0] keyboard;
    wire [14:0] size;
    wire [13:0] address;
    wire [7:0] data, peek_data;
    reg [15:0] peek_addr = 16'h0000;
    fes_computer_gp gp (.clk(clk), .gpo(gpo), .gpi(gpi), .build_id(128'b0),
        .exec_reset(reset), .keyboard(keyboard), .media_ready(ready),
        .media_size(size), .media_addr(address), .media_q(data));
    sg1000_machine machine (.clk_sys(clk), .reset(reset), .keyboard(keyboard),
        .media_ready(ready), .media_size(size), .media_addr(address), .media_data(data),
        .peek_addr(peek_addr), .peek_data(peek_data));
    reg toggle = 0;
    task exchange(input [7:0] opcode, input [7:0] index, input [15:0] argument);
        integer timeout;
        begin
            @(negedge clk);
            toggle = !toggle;
            gpo = {toggle, opcode[6:0], index, argument};
            timeout = 0;
            while (gpi[23] !== toggle && timeout < 16) begin
                @(negedge clk); timeout = timeout + 1;
            end
            if (gpi !== {8'hf5, toggle, 23'b0})
                $fatal(1, "GP response %h opcode %h", gpi, opcode);
        end
    endtask
    function [7:0] expected(input integer offset);
        expected = 8'h40 ^ offset[7:0] ^ offset[15:8];
    endfunction
    integer i, load, count;
    initial begin
      for (load = 0; load < 5; load = load + 1) begin
        case (load)
            0: count = 1;
            1: count = 3;
            2, 4: count = 256;
            3: count = 16384;
        endcase
        exchange(2, 0, 0);
        exchange(4, 0, count);
        for (i = 0; i + 1 < count; i = i + 2)
            exchange(5, 0, {expected(i + 1), expected(i)});
        if (i < count) exchange(5, 1, {8'b0, expected(i)});
        exchange(6, 0, 0);
        exchange(2, 0, 1);
        wait(machine.media_loaded);
        for (i = 0; i < count; i = i + 1) begin
            @(negedge clk); peek_addr = 16'h0000 + i;
            @(negedge clk);
            if (peek_data !== expected(i))
                $fatal(1, "Quartus cartridge[%0d]=%h expected=%h size=%0d",
                       i, peek_data, expected(i), count);
        end
        $display("Quartus vendor media: %0d exact bytes passed, immediate RELEASE", count);
      end
        $display("Quartus vendor GP media copy passed");
        $finish;
    end
    always @(negedge clk)
        if (!machine.media_loaded &&
            (machine.cpu.RESET_n !== 0 || machine.vdp.reset !== 1))
            $fatal(1, "Quartus CPU/VDP escaped reset before final copy");
    initial begin #100000000; $fatal(1, "media test timeout"); end
endmodule
