// SPDX-License-Identifier: GPL-2.0-or-later
// Spectrum beeper and edge-socket PCM. The beeper is the port $FE bit that
// software toggles; a coupling capacitor is modelled as a leaky integrator
// updated once per T-state. Socket audio is added with saturation.
module spectrum_audio (
    input  wire               clk,
    input  wire               reset,
    input  wire               cpu_cycle,
    input  wire               speaker,
    input  wire signed [15:0] slot_audio,
    output reg  signed [15:0] sample
);
    reg last = 1'b0;
    reg signed [23:0] level = 24'sd0;
    wire signed [23:0] step = speaker != last ? (speaker ? 24'sd4194304 : -24'sd4194304) : 24'sd0;
    reg signed [17:0] mixed = 18'sd0;

    always @(posedge clk) begin
        if (reset) begin
            last <= speaker;
            level <= 24'sd0;
        end else if (cpu_cycle) begin
            last <= speaker;
            level <= level + step - (level >>> 12);
        end
        mixed <= {{2{level[23]}}, level[23:8]} + {{2{slot_audio[15]}}, slot_audio};
        sample <= mixed > 18'sd32767 ? 16'sh7fff : mixed < -18'sd32768 ? -16'sh8000 : mixed[15:0];
    end

    initial sample = 16'sd0;
endmodule
