// SPDX-License-Identifier: GPL-2.0-or-later
// Apple II speaker and slot audio mix. The speaker is a one-bit output that
// software toggles through $C030; the original drives it through a coupling
// capacitor, modelled here as a DC-blocking leaky integrator updated once per
// CPU cycle (time constant 4096 cycles, about 4 ms). Slot cards contribute
// signed PCM through the bus response and are added with saturation. The
// result is held for fes_audio_output, which samples it at 48 kHz.
module apple2_audio (
    input  wire               clk,
    input  wire               reset,
    input  wire               cpu_cycle,
    input  wire               speaker,
    input  wire signed [15:0] slot_audio,
    output reg  signed [15:0] sample
);
    reg last = 1'b0;
    reg signed [23:0] level = 24'sd0;   // 16.8 fixed point
    wire signed [23:0] step = speaker != last ? (speaker ? 24'sd4194304 : -24'sd4194304) : 24'sd0;
    wire signed [17:0] mixed = {{2{level[23]}}, level[23:8]} + {{2{slot_audio[15]}}, slot_audio};

    always @(posedge clk) begin
        if (reset) begin
            last <= speaker;
            level <= 24'sd0;
        end else if (cpu_cycle) begin
            last <= speaker;
            level <= level + step - (level >>> 12);
        end
        sample <= mixed > 18'sd32767 ? 16'sh7fff : mixed < -18'sd32768 ? -16'sh8000 : mixed[15:0];
    end

    initial sample = 16'sd0;
endmodule
