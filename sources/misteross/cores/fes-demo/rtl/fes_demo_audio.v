// SPDX-License-Identifier: GPL-2.0-or-later
// Audio-domain tone source and CDC boundary for the pixel-domain GP endpoint.
module fes_demo_audio (
    input wire clk,
    input wire locked,
    input wire exec_reset,
    input wire [7:0] buttons,
    output wire sclk,
    output wire lrclk,
    output wire sdata
);
    (* async_reg = "true" *) reg hold_meta = 1;
    (* async_reg = "true" *) reg hold_sync = 1;
    (* async_reg = "true" *) reg right_meta = 0;
    (* async_reg = "true" *) reg right_sync = 0;
    (* async_reg = "true" *) reg lock_meta = 0;
    (* async_reg = "true" *) reg lock_sync = 0;
    always @(posedge clk) begin
        hold_meta <= exec_reset;
        hold_sync <= hold_meta;
        right_meta <= buttons[3];
        right_sync <= right_meta;
    end
    // Clock loss may stop clk. Assert asynchronously; release only after two
    // fresh audio edges, then restart the serializer at a known frame phase.
    always @(posedge clk or negedge locked) begin
        if (!locked) begin
            lock_meta <= 0;
            lock_sync <= 0;
        end else begin
            lock_meta <= 1;
            lock_sync <= lock_meta;
        end
    end
    wire tick;
    // A 96-sample cycle gives 1000 Hz left, 500 Hz right. Right doubles
    // both frequencies. Counter advances by two or one in a 96-step cycle.
    reg [6:0] cycle_phase = 0;
    wire [7:0] next_phase = {1'b0, cycle_phase} + (right_sync ? 8'd2 : 8'd1);
    wire [15:0] left_sample = (cycle_phase < 24 || (cycle_phase >= 48 && cycle_phase < 72)) ? 16'd4096 : 16'hf000;
    wire [15:0] right_sample = cycle_phase < 48 ? 16'd4096 : 16'hf000;
    always @(posedge clk) begin
        if (hold_sync || !lock_sync) cycle_phase <= 0;
        else if (tick) cycle_phase <= next_phase >= 96 ? 7'(next_phase - 96) : next_phase[6:0];
    end
    wire serial_data;
    assign sdata = locked && serial_data;
    fes_audio_i2s serializer (
        .clk(clk), .reset(!lock_sync), .mute(hold_sync),
        .left_sample(left_sample), .right_sample(right_sample), .sample_tick(tick),
        .sclk(sclk), .lrclk(lrclk), .sdata(serial_data)
    );
endmodule
