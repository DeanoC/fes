// SPDX-License-Identifier: GPL-2.0-or-later
// Spectrum sound: beeper, MIC and EAR, the 128K AY-3-8912 and edge-socket PCM.
//
// The beeper level is OUT bit 4 (two units) plus MIC low or an EAR input
// (one unit), as in Fuse; a coupling capacitor is a leaky integrator updated
// every T state. The AY's unsigned three-channel sum is DC-blocked with a
// much slower leak so its bass survives. Socket audio is added with
// saturation. mute (turbo loading) silences the output; the filters still
// follow every T state.
module spectrum_audio (
    input  wire               clk,
    input  wire               reset,
    input  wire               tick,
    input  wire               mute,
    input  wire               speaker,
    input  wire               mic,
    input  wire               ear,
    input  wire [7:0]         ay_pcm,
    input  wire signed [15:0] slot_audio,
    output reg  signed [15:0] sample = 16'sd0
);
    wire [1:0] beeper = {speaker, ~mic | ear};
    reg [1:0] last = 2'd0;
    reg signed [23:0] level = 24'sd0;
    // Three beeper units span the old 16384-step swing before the leak.
    function signed [23:0] beeper_level(input [1:0] units);
        case (units)
            2'd0: beeper_level = 24'sd0;
            2'd1: beeper_level = 24'sd1398101;
            2'd2: beeper_level = 24'sd2796203;
            default: beeper_level = 24'sd4194304;
        endcase
    endfunction
    wire signed [23:0] step = beeper_level(beeper) - beeper_level(last);

    // One AY step (of 255) is 64 output units.
    reg [7:0] ay_last = 8'd0;
    reg signed [28:0] ay_level = 29'sd0;
    wire signed [28:0] ay_step = ($signed({21'd0, ay_pcm}) - $signed({21'd0, ay_last})) <<< 18;

    reg signed [18:0] mixed = 19'sd0;
    always @(posedge clk) begin
        if (reset) begin
            last <= beeper;
            level <= 24'sd0;
            ay_last <= ay_pcm;
            ay_level <= 29'sd0;
        end else if (tick) begin
            last <= beeper;
            level <= level + step - (level >>> 12);
            ay_last <= ay_pcm;
            ay_level <= ay_level + ay_step - (ay_level >>> 16);
        end
        mixed <= {{3{level[23]}}, level[23:8]} + {{3{ay_level[28]}}, ay_level[27:12]} +
                 {{3{slot_audio[15]}}, slot_audio};
        sample <= mute ? 16'sd0 : mixed > 19'sd32767 ? 16'sh7fff : mixed < -19'sd32768 ? -16'sh8000 : mixed[15:0];
    end
endmodule
