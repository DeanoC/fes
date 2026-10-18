// SPDX-License-Identifier: GPL-2.0-or-later
// Development 8088 frontend. clk MUST be 100 MHz for the imported EU timing
// table. The fractional pin clock is data, never a second RTL clock domain.
module pcxt_cpu (
    input wire clk, reset, intr, nmi,
    output wire cycle_valid,
    input wire cycle_ready,
    output wire [19:0] cycle_address,
    output wire [7:0] cycle_wdata,
    output wire [1:0] cycle_kind,
    output wire cycle_write, cycle_lock,
    input wire cycle_done,
    input wire [7:0] cycle_rdata,
    input wire [1:0] cycle_fault,
    output wire [1:0] fault,
    output wire guest_ce,
    output wire halted
);
    reg [7:0] phase;
    reg pin_clock;
    wire [8:0] phase_sum = {1'b0, phase} + 9'd21;
    assign guest_ce = !reset && phase_sum >= 9'd220 && !pin_clock;
    always @(posedge clk) begin
        if (reset) begin
            phase <= 0;
            pin_clock <= 0;
        end else if (phase_sum >= 9'd220) begin
            phase <= 8'(phase_sum - 9'd220);
            pin_clock <= !pin_clock;
        end else phase <= phase_sum[7:0];
    end

    wire [19:0] ad;
    wire [2:0] status;
    wire data_phase, lock_n, cpu_ready;
    wire [7:0] cpu_rdata;
    // Status 011 is only the halt bus cycle. The bus unit may prefetch
    // (status 100) while the execution unit waits. The next other active
    // status is the wakeup.
    reg cpu_halted;
    assign halted = cpu_halted;
    always @(posedge clk) begin
        if (reset) cpu_halted <= 0;
        else if (status == 3'b011) cpu_halted <= 1;
        else if (status != 3'b111 && status != 3'b100) cpu_halted <= 0;
    end
    /* verilator lint_off PINCONNECTEMPTY */
    i8088 cpu (
        .CORE_CLK(clk), .CLK(pin_clock), .RESET(reset), .READY(cpu_ready),
        .INTR(intr), .NMI(nmi), .ad_out(ad), .dout(), .din(cpu_rdata),
        .lock_n(lock_n), .s6_3_mux(data_phase), .s2_s0_out(status),
        .SEGMENT(), .biu_done(), .cycle_accrate(1'b1),
        .clock_cycle_counter_division_ratio(8'd0),
        .clock_cycle_counter_decrement_value(8'd1), .shift_read_timing(1'b0),
        .is8086(1'b0), .fake286_flags(1'b0), .word_read_request(),
        .word_write_request(), .data_bus_word_out(), .data_bus_word(16'b0),
        .word_access_possible(1'b0)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    pcxt_cpu_bus bus_adapter (.*);
endmodule
