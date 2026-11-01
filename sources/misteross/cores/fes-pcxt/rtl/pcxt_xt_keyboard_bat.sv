// SPDX-License-Identifier: GPL-2.0-or-later
// XT keyboard reset response and arbitration for the host input frontend.
// All bytes travel over the existing timed cable and motherboard receiver.
module pcxt_xt_keyboard_bat (
    input wire clk, reset,
    input wire clock_low, data_low, keyboard_clear,
    input wire code_valid,
    input wire [7:0] code,
    output wire code_ready, frontend_reset,
    output wire keyboard_clock, keyboard_data
);
    // The DOS package and the existing serializer use the 100 MHz board clock.
    localparam [20:0] RESET_TICKS = 21'd2000000;
    reg [20:0] inhibit_ticks;
    reg pending;
    wire ready;
    // Hold the serializer reset throughout a long inhibit and its release
    // edge. This also cancels a previous frame when the host resets again.
    wire serial_reset = reset || inhibit_ticks == RESET_TICKS;
    assign frontend_reset = serial_reset;
    assign code_ready = ready && !pending && !serial_reset;
    always @(posedge clk) begin
        if (reset) begin
            inhibit_ticks <= 0;
            pending <= 0;
        end else if (clock_low) begin
            if (inhibit_ticks != RESET_TICKS) inhibit_ticks <= inhibit_ticks + 1'b1;
            if (inhibit_ticks == RESET_TICKS) pending <= 0;
        end else begin
            inhibit_ticks <= 0;
            if (inhibit_ticks == RESET_TICKS) pending <= 1;
            else if (pending && ready) pending <= 0;
        end
    end
    pcxt_xt_keyboard serializer (
        .clk(clk), .reset(serial_reset), .code_valid(pending || code_valid),
        .code(pending ? 8'haa : code), .code_ready(ready),
        .clock_low(clock_low), .data_low(data_low), .keyboard_clear(keyboard_clear),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data)
    );
endmodule
