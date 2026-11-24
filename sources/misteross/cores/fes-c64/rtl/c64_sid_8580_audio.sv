// SPDX-License-Identifier: GPL-2.0-or-later
// VICE 3.10 reSID Filter::solve_integrate_8580 / clock / output
// (default nonlinear filter, zero bias, disconnected EXT IN). The two
// capacitors integrate once per Phi. Measured op-amp transfer functions use
// interpolated knots; voice quantization uses the mean of reSID's dither.
module c64_sid_8580_audio(
    input wire clk, reset, phi, voices_ready,
    input wire [11:0] wave0, wave1, wave2,
    input wire [7:0] env0, env1, env2,
    input wire [10:0] fc,
    input wire [7:0] res_filt, mode_vol,
    output wire done, busy,
    output wire signed [31:0] sample
);
    // The terminated 2R/R=2 ladder is monotonic. reSID build_dac_table
    // includes 0.0035 MOSFET leakage and rounds to integer DAC codes.
    // The following are the transitions of DAC(code)-code for 12/8 bits.
    function automatic signed [12:0] centered_wave(input [11:0] code);
        reg signed [12:0] offset;
        begin
            if (code < 222) offset = 14;
            else if (code < 489) offset = 13;
            else if (code < 756) offset = 12;
            else if (code < 1023) offset = 11;
            else if (code < 1291) offset = 10;
            else if (code < 1558) offset = 9;
            else if (code < 1825) offset = 8;
            else if (code < 2092) offset = 7;
            else if (code < 2359) offset = 6;
            else if (code < 2626) offset = 5;
            else if (code < 2893) offset = 4;
            else if (code < 3161) offset = 3;
            else if (code < 3428) offset = 2;
            else if (code < 3695) offset = 1;
            else if (code < 3962) offset = 0;
            else offset = -1;
            centered_wave = $signed({1'b0, code}) + offset - 13'sh9e0;
        end
    endfunction
    function automatic [7:0] envelope_dac(input [7:0] code);
        envelope_dac = code < 53 ? code + 1'b1 :
                       code < 188 ? code : code - 1'b1;
    endfunction

    `include "c64_sid_8580_tables.vh"

    // Capture each digital voice after its oscillator and envelope have
    // advanced. One DAC/multiplier pipeline handles all three in succession.
    reg [4:0] voice_pipe = 0;
    reg [11:0] saved_wave1, saved_wave2;
    reg [7:0] saved_env1, saved_env2;
    wire [11:0] wave_in = voice_pipe[0] ? saved_wave1 :
                          voice_pipe[1] ? saved_wave2 : wave0;
    wire [7:0] env_in = voice_pipe[0] ? saved_env1 :
                        voice_pipe[1] ? saved_env2 : env0;
    reg signed [12:0] wave_dac;
    reg [7:0] env_dac;
    reg signed [20:0] voice_product;
    reg [15:0] voice_voltage, v0, v1, v2;
    always @(posedge clk) begin
        voice_pipe <= reset ? '0 : {voice_pipe[3:0], voices_ready};
        if (voices_ready) begin
            saved_wave1 <= wave1;
            saved_wave2 <= wave2;
            saved_env1 <= env1;
            saved_env2 <= env2;
        end
        wave_dac <= centered_wave(wave_in);
        env_dac <= envelope_dac(env_in);
        voice_product <= wave_dac * $signed({1'b0, env_dac});
        voice_voltage <= SID8580_VOICE_DC +
            16'((32'sd262144 + $signed(SID8580_VOICE_SCALE) * voice_product) >>> 18);
        if (voice_pipe[2]) v0 <= voice_voltage;
        if (voice_pipe[3]) v1 <= voice_voltage;
        if (voice_pipe[4]) v2 <= voice_voltage;
    end

    // Each Phi starts a 42-edge transaction. Integrator arithmetic overlaps
    // the motherboard's sequenced oscillator capture on Phi+10; the summer
    // consumes the finished voltage of all three voices on Phi+21.
    reg [5:0] step = 0;
    reg [3:0] resonance, volume;
    reg [2:0] routed, direct, outputs;
    reg [3:0] summer_count, mixer_count;
    reg [15:0] lp, bp, hp, lp_x, bp_x;
    reg signed [31:0] lp_charge, bp_charge;
    reg signed [16:0] lp_gs, lp_gd, bp_gs, bp_gd;
    reg [13:0] wl;
    reg [15:0] n_dac;
    wire [2:0] direct_mask = ~res_filt[2:0] & {~mode_vol[7],2'b11};
    function automatic [3:0] count3(input [2:0] bits);
        count3 = {3'd0,bits[0]} + {3'd0,bits[1]} + {3'd0,bits[2]};
    endfunction

    // Difference of squares via (gs + gd)*(gs - gd), followed by the
    // reference's 32-bit wrap. gd is zero when the transistor saturates.
    // One pipelined 18x18 unit also handles current scaling for both caps.
    reg signed [17:0] mul_a, mul_b;
    reg [17:0] p00;
    reg signed [18:0] p10,p01;
    reg signed [17:0] p11;
    reg [17:0] lo;
    reg signed [19:0] mid;
    reg signed [17:0] hi;
    reg signed [31:0] product;
    always @* begin
        mul_a = 0; mul_b = 0;
        case (step)
            1: begin mul_a = lp_gs + lp_gd; mul_b = lp_gs - lp_gd; end
            2: begin mul_a = bp_gs + bp_gd; mul_b = bp_gs - bp_gd; end
            4,5: begin mul_a = 18'(product >>> 15); mul_b = $signed({1'b0,n_dac}); end
            default: ;
        endcase
    end
    always @(posedge clk) begin
        p00 <= mul_a[8:0] * mul_b[8:0];
        p01 <= $signed({1'b0,mul_a[8:0]}) * $signed(mul_b[17:9]);
        p10 <= $signed(mul_a[17:9]) * $signed({1'b0,mul_b[8:0]});
        p11 <= $signed(mul_a[17:9]) * $signed(mul_b[17:9]);
        lo <= p00; mid <= p10 + p01; hi <= p11;
        product <= $signed({1'b0,lo}) + (32'(mid) <<< 9) + (32'(hi) <<< 18);
    end

    wire [18:0] filter_voices = (routed[0] ? v0 : 0) + (routed[1] ? v1 : 0) +
                                (routed[2] ? v2 : 0);
    wire [18:0] direct_voices = (direct[0] ? v0 : 0) + (direct[1] ? v1 : 0) +
                                (direct[2] ? v2 : 0);
    wire [18:0] filter_outputs = (outputs[0] ? lp : 0) + (outputs[1] ? bp : 0) +
                                 (outputs[2] ? hp : 0);
    reg [15:0] resonance_voltage, mixer_voltage;
    reg [2:0] curve_family;
    reg [3:0] curve_selector;
    reg [18:0] lookup_index;
    wire [15:0] lookup_result;
    // Lookup latency is five edges from issue to use. MIXER[0] is a
    // single tabulated voltage with no input when every source is muted.
    always @* begin
        curve_family = 0; curve_selector = 0; lookup_index = 0;
        case (step)
            8: lookup_index = {3'd0,~lp_charge[30],lp_charge[29:15]};
            9: lookup_index = {3'd0,~bp_charge[30],bp_charge[29:15]};
            15: begin curve_family=1; curve_selector=resonance; lookup_index=bp; end
            21: begin curve_family=2; curve_selector=summer_count; lookup_index=resonance_voltage+lp+filter_voices; end
            27: begin curve_family=3; curve_selector=mixer_count; lookup_index=direct_voices+filter_outputs; end
            33: begin curve_family=4; curve_selector=volume; lookup_index=mixer_voltage; end
            default: ;
        endcase
    end
    c64_sid_8580_lookup tables(.clk(clk),.curve_family(curve_family),.curve_selector(curve_selector),
        .index(lookup_index),.value(lookup_result));

    reg signed [15:0] internal_sample;
    c64_sid_output_filter output_network(
        .clk(clk), .reset(reset), .start(step == 39),
        .signal_in(internal_sample), .done(done), .busy(),
        .sample(sample), .low_state(), .high_state()
    );
    assign busy = step != 0;
    always @(posedge clk) begin
        if (reset) begin
            step <= 0;
            hp <= 0; bp <= 0; lp <= 0;
            lp_charge <= 0; bp_charge <= 0;
            lp_x <= 0; bp_x <= 0;
        end else if (phi) begin
            step <= 1;
            routed <= res_filt[2:0]; direct <= direct_mask; outputs <= mode_vol[6:4];
            resonance <= res_filt[7:4]; volume <= mode_vol[3:0];
            summer_count <= count3(res_filt[2:0]);
            mixer_count <= count3(direct_mask) + count3(mode_vol[6:4]);
            // VICE's 11-bit frequency DAC sums parallel channel widths.
            wl <= fc == 0 ? 14'd3 : 14'((24'd1612 * fc) >> 8);
            lp_gs <= SID8580_N_VGT - lp_x;
            lp_gd <= bp < SID8580_N_VGT ? SID8580_N_VGT - bp : 0;
            bp_gs <= SID8580_N_VGT - bp_x;
            bp_gd <= hp < SID8580_N_VGT ? SID8580_N_VGT - hp : 0;
        end else if (step != 0) begin
            step <= step == 42 ? 0 : step + 1'b1;
            case (step)
                1: n_dac <= 16'((32'(SID8580_N_PARAM) * wl) >> 11);
                7: lp_charge <= lp_charge - (product >>> 4);
                8: bp_charge <= bp_charge - (product >>> 4);
                13: begin lp_x <= lookup_result; lp <= lookup_result + 16'(lp_charge >>> 14); end
                14: begin bp_x <= lookup_result; bp <= lookup_result + 16'(bp_charge >>> 14); end
                20: resonance_voltage <= lookup_result;
                26: hp <= lookup_result;
                32: mixer_voltage <= mixer_count == 0 ? SID8580_MIXER_ZERO : lookup_result;
                38: internal_sample <= lookup_result ^ 16'h8000;
                default: ;
            endcase
        end
    end
endmodule
