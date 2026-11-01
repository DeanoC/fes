// SPDX-License-Identifier: GPL-2.0-or-later
// Timed XT set-1 scan-code serializer. The producer holds each byte until
// READY; translation and repeat generation are outside this byte transport.
module pcxt_xt_keyboard (
    input wire clk, reset,
    input wire code_valid,
    input wire [7:0] code,
    output wire code_ready,
    input wire clock_low, data_low, keyboard_clear,
    output reg keyboard_clock, keyboard_data
);
    // 40 us high + 40 us low, within IBM's 25..50 us active-clock range.
    // An RTS low pulse precedes the start bit and waits for the host CTS.
    localparam IDLE=2'd0, RTS=2'd1, HIGH=2'd2, LOW=2'd3;
    reg [1:0] state;
    reg [12:0] phase;
    reg [8:0] frame;
    reg [3:0] index;
    // READY means the frontend may retire the byte. PB7 (keyboard_clear) resets
    // the receiver without clamping clock or data, so a cable keyboard can still
    // shift a byte that never raises IRQ1. Do not accept while that reset is held.
    // A byte already accepted still finishes its frame.
    assign code_ready = !reset && state == IDLE && !clock_low && !data_low && !keyboard_clear;
    always @(posedge clk) begin
        if (reset) begin
            state <= IDLE; phase <= 0; frame <= 0; index <= 0;
            keyboard_clock <= 1; keyboard_data <= 0;
        end else case (state)
            IDLE: if (code_valid && code_ready) begin
                frame <= {code, 1'b1}; index <= 0; phase <= 0;
                keyboard_clock <= 0; keyboard_data <= 0; state <= RTS;
            end
            RTS: if (!clock_low && !data_low) begin
                // 80 us between RTS/CTS and the first active clock edge.
                if (phase == 299) keyboard_data <= frame[0];
                if (phase == 7999) begin
                    keyboard_clock <= 1;
                    phase <= 0; state <= HIGH;
                end else phase <= phase + 1'b1;
            end else phase <= 0;
            HIGH: if (!clock_low) begin
                if (phase == 3999) begin
                    keyboard_clock <= 0; phase <= 0; state <= LOW;
                end else phase <= phase + 1'b1;
            end
            LOW: if (!clock_low) begin
                // Hold the sampled bit for 3 us, then provide 37 us setup
                // before the next rising edge (IBM requires at least 2.5 us).
                if (phase == 299) keyboard_data <= index == 8 ? 1'b0 : frame[1];
                if (phase == 3999) begin
                    phase <= 0;
                    if (index == 8) begin
                        keyboard_clock <= 1; keyboard_data <= 0; state <= IDLE;
                    end else begin
                        index <= index + 1'b1; frame <= {1'b0, frame[8:1]};
                        keyboard_clock <= 1; state <= HIGH;
                    end
                end else phase <= phase + 1'b1;
            end
            default: state <= IDLE;
        endcase
    end
endmodule
