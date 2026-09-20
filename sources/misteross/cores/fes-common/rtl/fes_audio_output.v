// SPDX-License-Identifier: GPL-2.0-or-later
// Coherent stereo PCM crossing into the existing 12.288 MHz I2S serializer.
// Source must keep running. A request/ack transfer holds both words together
// until captured; unlike bitwise synchronizers this cannot tear a PCM sample.
module fes_audio_output (
    input wire source_clk, audio_clk, locked, hold,
    input wire [15:0] left_sample, right_sample,
    output wire sclk, lrclk, sdata
);
    reg request = 0;
    (* async_reg = "true" *) reg request_meta = 0, request_sync = 0;
    reg acknowledge = 0;
    reg [31:0] held_sample = 0;
    always @(posedge source_clk) begin
        request_meta <= request;
        request_sync <= request_meta;
        if (request_sync != acknowledge) begin
            held_sample <= {left_sample, right_sample};
            acknowledge <= request_sync;
        end
    end
    (* async_reg = "true" *) reg ack_meta = 0, ack_sync = 0;
    (* async_reg = "true" *) reg hold_meta = 1, hold_sync = 1;
    (* async_reg = "true" *) reg lock_meta = 0, lock_sync = 0;
    reg [31:0] pcm = 0;
    always @(posedge audio_clk or negedge locked) begin
        if (!locked) begin lock_meta <= 0; lock_sync <= 0; end
        else begin lock_meta <= 1; lock_sync <= lock_meta; end
    end
    wire tick;
    always @(posedge audio_clk) begin
        ack_meta <= acknowledge; ack_sync <= ack_meta;
        hold_meta <= hold; hold_sync <= hold_meta;
        if (ack_sync == request) begin
            pcm <= held_sample;
            // One round trip per output frame. Transfer state survives lock
            // loss so restart cannot confuse an old acknowledgment for new PCM.
            if (tick) request <= ~request;
        end
    end
    wire serial_data;
    assign sdata = locked && serial_data;
    fes_audio_i2s serializer (
        .clk(audio_clk), .reset(!lock_sync), .mute(hold_sync),
        .left_sample(pcm[31:16]), .right_sample(pcm[15:0]),
        .sample_tick(tick), .sclk(sclk), .lrclk(lrclk), .sdata(serial_data)
    );
endmodule
