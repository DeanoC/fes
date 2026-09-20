// SPDX-License-Identifier: GPL-2.0-or-later
// Synchronize an event toggle, then emit a bounded stereo success chime.
module fes_catch_audio (
    input wire clk, locked, exec_reset, sound_toggle,
    output wire sclk, lrclk, sdata
);
    (* async_reg = "true" *) reg event_meta = 0, event_sync = 0;
    (* async_reg = "true" *) reg hold_meta = 1, hold_sync = 1;
    (* async_reg = "true" *) reg lock_meta = 0, lock_sync = 0;
    reg previous_event = 0;
    reg [12:0] remaining = 0;
    reg [5:0] phase = 0;
    wire tick, serial_data;
    always @(posedge clk) begin
        event_meta <= sound_toggle; event_sync <= event_meta;
        hold_meta <= exec_reset; hold_sync <= hold_meta;
        previous_event <= event_sync;
        if (hold_sync || !lock_sync) begin remaining <= 0; phase <= 0; end
        else if (previous_event != event_sync) begin remaining <= 4800; phase <= 0; end
        else if (tick && remaining != 0) begin
            remaining <= remaining - 1'b1;
            phase <= phase == 47 ? 6'd0 : phase + 1'b1;
        end
    end
    always @(posedge clk or negedge locked) begin
        if (!locked) begin lock_meta <= 0; lock_sync <= 0; end
        else begin lock_meta <= 1; lock_sync <= lock_meta; end
    end
    wire [15:0] sample_value = phase < 24 ? 16'd4096 : 16'hf000;
    assign sdata = locked && serial_data;
    fes_audio_i2s serializer (.clk(clk), .reset(!lock_sync),
        .mute(hold_sync || remaining == 0), .left_sample(sample_value), .right_sample(sample_value),
        .sample_tick(tick), .sclk(sclk), .lrclk(lrclk), .sdata(serial_data));
endmodule
