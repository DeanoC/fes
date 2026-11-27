// SPDX-License-Identifier: GPL-2.0-or-later
// One bounded, destructive board-SDRAM diagnostic per execution release.
// The parent holds execution until the operator launches the package. A hold
// request stops issuing work; drained is false until the scan has retired all
// accepted traffic, including when the target stops responding.
module fes_de25_managed_ram_scan #(
    parameter [31:0] TEST_BYTES = 32'h08000000,
    parameter integer MAX_BURST = 16,
    parameter integer TIMEOUT = 1000000,
    parameter integer LATENCY_SAMPLES = 1024
) (
    input wire clk, reset_n, run_permitted, hold_requested, calibrated,
    output wire drained,
    // 0 waiting, 1 running, 2 pass, 3 fail, 4 stopped. LPDDR is not tested.
    output wire [2:0] result,
    (* preserve, dont_merge *) output reg [4:0] result_flags = 5'b00001,
    output wire [31:0] address,
    output wire [4:0] burstcount,
    output wire read, write,
    output wire [15:0] writedata,
    output wire [1:0] byteenable,
    input wire waitrequest, readdatavalid, writeresponsevalid,
    input wire [15:0] readdata,
    input wire [1:0] response
);
    reg started = 0;
    reg observed_busy = 0;
    reg start = 0;
    wire busy, done, stopped;
    wire [31:0] faults, errors;
    wire stopping = hold_requested || !run_permitted;
    assign drained = !busy && !start;
    assign result = (faults != 0 || errors != 0) ? 3'd3 :
                    !started ? 3'd0 : !observed_busy ? 3'd1 :
                    stopped ? 3'd4 : done ? 3'd2 : 3'd1;
    // Registered one-hot observations cross individually into the video clock.
    always @(posedge clk) begin
        case (result)
            3'd1: result_flags <= 5'b00010;
            3'd2: result_flags <= 5'b00100;
            3'd3: result_flags <= 5'b01000;
            3'd4: result_flags <= 5'b10000;
            default: result_flags <= 5'b00001;
        endcase
        start <= 1'b0;
        if (!run_permitted && drained) begin
            started <= 1'b0;
            observed_busy <= 1'b0;
        end else if (busy) observed_busy <= 1'b1;
        if (reset_n && !started && run_permitted && !hold_requested && calibrated) begin
            started <= 1'b1;
            start <= 1'b1;
        end
    end
    fes_de25_ram_scan #(.DATA_W(16), .MAX_BURST(MAX_BURST), .TIMEOUT(TIMEOUT),
        .LATENCY_SAMPLES(LATENCY_SAMPLES), .CHECK_ADDRESS_HIGH(1)) scan (
        .clk(clk), .reset_n(reset_n), .start(start), .stop(stopping), .clear(1'b0),
        .calibrated(calibrated), .base(32'd0), .length(TEST_BYTES),
        .busy(busy), .done(done), .stopped(stopped), .faults(faults),
        .errors(errors), .pattern(), .cursor(), .first_addr(), .first_expected(),
        .first_actual(), .bad_bits(), .written_bytes(), .read_bytes(),
        .write_cycles(), .read_cycles(), .samples(), .first_min(), .first_max(),
        .last_min(), .last_max(), .first_sum(), .last_sum(),
        .address(address), .burstcount(burstcount), .read(read), .write(write),
        .writedata(writedata), .byteenable(byteenable), .waitrequest(waitrequest),
        .readdatavalid(readdatavalid), .writeresponsevalid(writeresponsevalid),
        .readdata(readdata), .response(response)
    );
endmodule
