// SPDX-License-Identifier: MIT
// pc386 mixer (docs/pc386-machine-design.md, "Mixer"): the PC speaker, the
// OPL2 and the DSP, each held at its current value, summed with fixed gains
// and integrated at system-clock resolution over every 48 kHz output period,
// then divided by the period's length in clocks. That box filter is the one
// resampler for the 1-bit speaker, the 49.7 kHz OPL2 and the DSP's rate.
//
// Gains: OPL2 and DSP at half scale (-6 dBFS for a full-scale source), the
// speaker 16384 when on (a full square wave swings +-8192 after the DC
// blocker, -12 dBFS). A one-pole DC blocker near 30 Hz (k = 1/256 at 48 kHz)
// removes static cone drive and offsets; the sum saturates to 16 bits.
//
// request is the output's frame request, one clock, already synchronised to
// this clock; the sample follows it after a 33-clock division.
module pc386_mixer (
    input wire clk, reset,
    input wire request,
    input wire signed [15:0] opl2, dsp,
    input wire speaker,
    output reg sample_valid,
    output reg signed [15:0] sample
);
    // Sum per clock: |v| <= 3 * 16384, so 17 bits; 4095 clocks fit 30 bits.
    wire signed [16:0] level = 17'(opl2 >>> 1) + 17'(dsp >>> 1) + (speaker ? 17'sd16384 : 17'sd0);
    reg signed [31:0] area;
    reg [11:0] clocks;

    // ---- Division of the period's area by its length ---------------------------
    reg dividing, negative;
    reg [5:0] step;
    reg [31:0] dividend, quotient;
    reg [12:0] remainder;
    reg [11:0] divisor;
    wire [12:0] trial = {remainder[11:0], dividend[31]};
    wire take = trial >= {1'b0, divisor};

    // ---- DC blocker ---------------------------------------------------------------
    reg signed [27:0] dc;
    wire signed [27:0] mean = negative ? -28'(quotient) : 28'(quotient);
    wire signed [27:0] error = (mean <<< 8) - dc;
    wire signed [19:0] high_pass = 20'(error >>> 8);

    always @(posedge clk) begin
        sample_valid <= 0;
        if (reset) begin
            area <= 0; clocks <= 0;
            dividing <= 0; negative <= 0; step <= 0;
            dividend <= 0; quotient <= 0; remainder <= 0; divisor <= 1;
            dc <= 0; sample <= 0;
        end else begin
            if (request) begin
                area <= 32'(level);
                clocks <= 1;
                dividing <= 1;
                step <= 0;
                negative <= area < 0;
                dividend <= area < 0 ? -area : area;
                remainder <= 0;
                quotient <= 0;
                divisor <= clocks == 0 ? 12'd1 : clocks;
            end else begin
                area <= area + 32'(level);
                if (clocks != 12'hfff) clocks <= clocks + 1'b1;
            end
            if (dividing && !request) begin
                remainder <= take ? trial - {1'b0, divisor} : trial;
                quotient <= {quotient[30:0], take};
                dividend <= {dividend[30:0], 1'b0};
                step <= step + 1'b1;
                if (step == 6'd31) dividing <= 0;
            end
            if (!dividing && step == 6'd32) begin
                step <= 0;
                dc <= dc + 28'(error >>> 8);
                sample <= high_pass > 20'sd32767 ? 16'sd32767 : high_pass < -20'sd32768 ? -16'sd32768 : 16'(high_pass);
                sample_valid <= 1;
            end
        end
    end
endmodule
