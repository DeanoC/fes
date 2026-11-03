// SPDX-License-Identifier: MIT
// #1457's unit bench: KF8253 counter 2 and port 61h bits 0-1, wired as
// pc386_at_core.sv wires them (100 MHz system clock, PIT clock from
// pc386_rate 21/880), driven with Ultima VI's step routine's I/O pattern:
// 3Ch then 00h to port 42h and no control word, then 2,000 steps 20 us
// apart. Each step sets or clears port 61h bits 0 and 1 together, as the
// lute's call chooses (dx += 3116; on when dx > bx; bx = 20000 - 10k).
//
// +mode=boot starts from KF8253's reset state, which pc386 had at the DOS
// prompt. +mode=at first writes an AT POST beep's setup (B6h, 33h, 05h)
// with the gate and speaker off. It prints the speaker's high time over each
// of two notes and writes trace-<mode>.txt: time, gate, speaker enable and
// OUT2 at each rising PIT clock edge. compare_8254.py checks the at trace
// against an 8254 model.
//
// From the repository root:
//   V=sources/misteross/cores; verilator --binary --timing -Wno-fatal -Wno-lint -Wno-style \
//     -I$V/fes-common/vendor/kf8253 --top-module tb \
//     docs/validation/2026-11-03-pc386-speaker-timer/kf8253_routine_bench.sv \
//     $V/fes-pc386/rtl/pc386_rate.sv $V/fes-common/vendor/kf8253/KF8253*.sv -o tb
//   obj_dir/tb +mode=boot; obj_dir/tb +mode=at
`timescale 1ns/1ps
module tb;
    logic clk = 0, reset = 1;
    always #5 clk = !clk;
    wire pit_tick;
    logic pit_clock;
    pc386_rate #(.N(21), .D(880)) rate (.clk(clk), .reset(reset), .tick(pit_tick));
    always @(posedge clk) if (reset) pit_clock <= 0; else if (pit_tick) pit_clock <= !pit_clock;
    logic cs_n = 1, we_n = 1;
    logic [1:0] address = 0;
    logic [7:0] data = 0;
    logic [3:0] port_b = 0;
    wire [7:0] pit_data;
    wire out0, out1, out2;
    KF8253 #(.READ_BACK(1)) pit (
        .clock(clk), .reset(reset), .chip_select_n(cs_n), .read_enable_n(1'b1), .write_enable_n(we_n),
        .address(address), .data_bus_in(data), .data_bus_out(pit_data),
        .counter_0_clock(pit_clock), .counter_0_gate(1'b1), .counter_0_out(out0),
        .counter_1_clock(pit_clock), .counter_1_gate(1'b1), .counter_1_out(out1),
        .counter_2_clock(pit_clock), .counter_2_gate(port_b[0]), .counter_2_out(out2));
    wire speaker = out2 && port_b[1];

    // A pc386_io-like write: chip select for 10 clocks, the strobe in 1-5.
    task automatic pit_write(input [1:0] a, input [7:0] d);
        @(posedge clk); cs_n <= 0; address <= a; data <= d;
        @(posedge clk); we_n <= 0;
        repeat (5) @(posedge clk);
        we_n <= 1;
        repeat (2) @(posedge clk);
        cs_n <= 1;
        repeat (2) @(posedge clk);
    endtask

    integer trace_fd;
    logic tracing = 0;
    always @(posedge pit_clock) if (tracing) $fwrite(trace_fd, "%0d %0d %0d %0d\n", $time, port_b[0], port_b[1], out2);

    longint high_ns, total_ns;
    logic counting = 0;
    always @(posedge clk) if (counting) begin total_ns += 10; if (speaker) high_ns += 10; end

    task automatic note(input integer step, input integer bx0, input integer inc, input integer steps, input integer step_ns);
        integer k; logic [15:0] dx, bx; logic on;
        pit_write(2'd2, 8'h3c);
        pit_write(2'd2, 8'h00);
        dx = 0; bx = 16'(bx0);
        for (k = 0; k < steps; k++) begin
            dx = dx + 16'(step);
            on = dx > bx;
            @(posedge clk);
            port_b <= on ? (port_b | 4'b0011) : (port_b & 4'b1100);
            bx = bx + 16'(inc);
            #(step_ns);
        end
        @(posedge clk); port_b <= port_b & 4'b1100;
    endtask

    string mode;
    initial begin
        if (!$value$plusargs("mode=%s", mode)) mode = "boot";
        trace_fd = $fopen({"trace-", mode, ".txt"}, "w");
        repeat (10) @(posedge clk);
        reset = 0;
        repeat (100) @(posedge clk);
        if (mode == "at") begin
            pit_write(2'd3, 8'hb6);
            pit_write(2'd2, 8'h33);
            pit_write(2'd2, 8'h05);
        end
        #(10000);
        tracing = 1; high_ns = 0; total_ns = 0; counting = 1;
        note(3116, 20000, -10, 2000, 20000);
        counting = 0; tracing = 0;
        $display("%s note 1: speaker high %0d of %0d ns (%0.1f%%)", mode, high_ns, total_ns, 100.0 * high_ns / total_ns);
        #(500000);
        high_ns = 0; total_ns = 0; counting = 1;
        note(3116, 20000, -10, 2000, 20000);
        counting = 0;
        $display("%s note 2: speaker high %0d of %0d ns (%0.1f%%)", mode, high_ns, total_ns, 100.0 * high_ns / total_ns);
        $fclose(trace_fd);
        $finish;
    end
endmodule
