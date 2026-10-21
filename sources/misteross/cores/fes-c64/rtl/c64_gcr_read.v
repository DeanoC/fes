// SPDX-License-Identifier: GPL-2.0-or-later
// 1541 digital read channel. ref_ce advances the 16 MHz reference clock.
// Flux pulses reset the density divider and data window; recovered bits are
// sampled midway through each cell. No analogue noise/write channel is modeled.
module c64_gcr_read (
    input wire clk, reset, ref_ce, flux, motor, reading, ready_enable,
    input wire [1:0] density,
    output wire [7:0] data,
    output wire sync_found, byte_ready_n,
    output reg bit_sample
);
    reg [3:0] divider, window;
    reg [9:0] shifter;
    reg [2:0] bit_count;
    reg ready;
    wire divider_end = divider == 4'hf;
    wire shift_now = divider_end && window[1:0] == 2'b01;
    wire recovered_bit = window[3:2] == 2'b00;
    wire [9:0] shifted = {shifter[8:0], recovered_bit};
    assign data = shifter[7:0];
    assign sync_found = &shifter;
    assign byte_ready_n = !(ready && ready_enable && motor && reading && !sync_found);
    always @(posedge clk) begin
        bit_sample <= 0;
        if (reset) begin
            divider <= {2'b00,density}; window <= 0; shifter <= 0;
            bit_count <= 0; ready <= 0;
        end else if (ref_ce) begin
            if (!motor || !reading) begin
                divider <= {2'b00,density}; window <= 0;
                bit_count <= 0; ready <= 0; shifter <= 0;
            end else if (flux) begin
                divider <= {2'b00,density}; window <= 0;
            end else begin
                if (divider_end) begin
                    divider <= {2'b00,density}; window <= window + 4'd1;
                end else divider <= divider + 4'd1;
                if (shift_now) begin
                    bit_sample <= 1;
                    shifter <= shifted;
                    ready <= 0;
                    if (&shifted) bit_count <= 0;
                    else begin
                        bit_count <= bit_count + 3'd1;
                        if (bit_count == 3'd7) ready <= 1;
                    end
                end
            end
        end
    end
endmodule
