// SPDX-License-Identifier: GPL-2.0-or-later
// AY-3-8912 digital sound engine. clk is the FPGA transport clock; chip_ce
// marks a rising edge of the card's CPU-clock/2 input. Decimal register IDs.
// GI data manual supplies register masks, mixer and envelope shapes. Tone
// counters count up, including during period changes; period zero is one.
// Envelope zero advances twice as fast as one (AY hardware behavior).
// YM2149 mode shares tone/noise/mixer logic and adds its 32-step envelope,
// both I/O register latches and nominal 1.5 dB envelope DAC steps. Default
// zero retains the AY8912 card behavior and its existing public ports.
// Yamaha YM2149 data sheet, level/envelope control sections:
// https://www.ym2149.com/ym2149.pdf
module zonx_ay #(parameter integer YM2149 = 0) (
    input wire clk, reset_n, chip_ce,
    input wire address_write, data_write,
    input wire [7:0] data,
    output wire [7:0] read_data,
    output wire [7:0] pcm
);
    reg [3:0] selected = 0;
    reg selected_valid = 1;
    reg [7:0] registers [0:15];
    reg [2:0] prescale = 0;
    reg half_phase = 0;
    reg [11:0] tone_count [0:2];
    reg [2:0] tone = 0;
    reg [4:0] noise_count = 1;
    reg [16:0] noise = 17'd1;
    reg [15:0] envelope_count = 1;
    reg envelope_half = 0;
    localparam [4:0] ENVELOPE_MAX = (YM2149 != 0) ? 5'd31 : 5'd15;
    reg [4:0] envelope_level = 0;
    reg envelope_up = 0, envelope_holding = 1;
    integer i;
    initial begin
        for (i = 0; i < 16; i = i + 1) registers[i] = 0;
        for (i = 0; i < 3; i = i + 1) tone_count[i] = 1;
    end
    function [7:0] mask_register;
        input [3:0] address;
        input [7:0] value;
        begin
            case (address)
                1, 3, 5, 13: mask_register = value & 8'h0f;
                6, 8, 9, 10: mask_register = value & 8'h1f;
                // R15 has no I/O port on the 8912. R14 is an unconnected
                // port latch; neither port contributes to sound.
                15: mask_register = (YM2149 != 0) ? value : 0;
                default: mask_register = value;
            endcase
        end
    endfunction
    wire [11:0] period_a = {registers[1][3:0], registers[0]};
    wire [11:0] period_b = {registers[3][3:0], registers[2]};
    wire [11:0] period_c = {registers[5][3:0], registers[4]};
    wire [15:0] envelope_period = {registers[12], registers[11]};
    wire restart = data_write && selected_valid && selected == 13;
    wire tick8 = chip_ce && prescale == 7;
    always @(posedge clk) begin
        if (!reset_n) begin
            selected <= 0;
            selected_valid <= 1;
            for (i = 0; i < 16; i = i + 1) registers[i] <= 0;
            for (i = 0; i < 3; i = i + 1) tone_count[i] <= 1;
            prescale <= 0;
            half_phase <= 0;
            tone <= 0;
            noise_count <= 1;
            noise <= 17'd1;
            envelope_count <= 1;
            envelope_half <= 0;
            envelope_level <= 0;
            envelope_up <= 0;
            envelope_holding <= 1;
        end else begin
            if (address_write) begin
                selected <= data[3:0];
                // GI Register Array: DA7..DA4 are chip-select address bits;
                // the standard AY8912 recognizes 0000, not arbitrary bits.
                selected_valid <= data[7:4] == 0;
            end
            if (data_write && selected_valid)
                registers[selected] <= mask_register(selected, data);
            if (chip_ce) prescale <= prescale + 1'b1;
            if (tick8) begin
                half_phase <= ~half_phase;
                if (tone_count[0] >= period_a) begin
                    tone_count[0] <= 1; tone[0] <= ~tone[0];
                end else tone_count[0] <= tone_count[0] + 1'b1;
                if (tone_count[1] >= period_b) begin
                    tone_count[1] <= 1; tone[1] <= ~tone[1];
                end else tone_count[1] <= tone_count[1] + 1'b1;
                if (tone_count[2] >= period_c) begin
                    tone_count[2] <= 1; tone[2] <= ~tone[2];
                end else tone_count[2] <= tone_count[2] + 1'b1;
                if (half_phase) begin
                    if (noise_count >= registers[6][4:0]) begin
                        noise_count <= 1;
                        noise <= {noise[0] ^ noise[3], noise[16:1]};
                    end else noise_count <= noise_count + 1'b1;
                end
            end
            // A bus transaction generates one restart, even when /WR is
            // asserted across many transport cycles. Writes win over ticks.
            if (restart) begin
                envelope_count <= 1;
                envelope_half <= 0;
                envelope_level <= data[2] ? 0 : ENVELOPE_MAX;
                envelope_up <= data[2];
                envelope_holding <= 0;
            end else if (tick8) begin
                envelope_half <= ~envelope_half;
                if ((YM2149 != 0) || envelope_period == 0 || envelope_half) begin
                    if (envelope_count >= envelope_period) begin
                        envelope_count <= 1;
                        if (!envelope_holding) begin
                            if ((envelope_up && envelope_level == ENVELOPE_MAX) ||
                                (!envelope_up && envelope_level == 0)) begin
                                if (!registers[13][3]) begin
                                    envelope_level <= 0;
                                    envelope_holding <= 1;
                                end else if (registers[13][0]) begin
                                    if (registers[13][1]) envelope_level <= (envelope_level ^ ENVELOPE_MAX);
                                    envelope_holding <= 1;
                                end else if (registers[13][1]) begin
                                    envelope_up <= ~envelope_up;
                                end else envelope_level <= envelope_up ? 0 : ENVELOPE_MAX;
                            end else if (envelope_up)
                                envelope_level <= envelope_level + 1'b1;
                            else envelope_level <= envelope_level - 1'b1;
                        end
                    end else envelope_count <= envelope_count + 1'b1;
                end
            end
        end
    end
    // A nominal 3 dB/step logarithmic DAC approximation, 85 per channel leaves room
    // for all three channels in the shell's unsigned 8-bit mono transport.
    function [7:0] amplitude;
        input [3:0] level;
        begin
            case (level)
                0: amplitude=0; 1: amplitude=1; 2: amplitude=1;
                3: amplitude=1; 4: amplitude=2; 5: amplitude=3;
                6: amplitude=4; 7: amplitude=5; 8: amplitude=8;
                9: amplitude=11; 10: amplitude=15; 11: amplitude=21;
                12: amplitude=30; 13: amplitude=43; 14: amplitude=60;
                15: amplitude=85;
            endcase
        end
    endfunction
    function [7:0] ym_amplitude;
        input [4:0] level;
        begin
            case (level)
                0: ym_amplitude=0; 1: ym_amplitude=0; 2: ym_amplitude=1;
                3: ym_amplitude=1; 4: ym_amplitude=1; 5: ym_amplitude=1;
                6: ym_amplitude=1; 7: ym_amplitude=1; 8: ym_amplitude=2;
                9: ym_amplitude=2; 10: ym_amplitude=2; 11: ym_amplitude=3;
                12: ym_amplitude=3; 13: ym_amplitude=4; 14: ym_amplitude=5;
                15: ym_amplitude=5; 16: ym_amplitude=6; 17: ym_amplitude=8;
                18: ym_amplitude=9; 19: ym_amplitude=11; 20: ym_amplitude=13;
                21: ym_amplitude=15; 22: ym_amplitude=18; 23: ym_amplitude=21;
                24: ym_amplitude=25; 25: ym_amplitude=30; 26: ym_amplitude=36;
                27: ym_amplitude=43; 28: ym_amplitude=51; 29: ym_amplitude=60;
                30: ym_amplitude=72; 31: ym_amplitude=85;
            endcase
        end
    endfunction
    function [7:0] channel_amplitude;
        input [4:0] setting;
        begin
            if (YM2149 != 0)
                channel_amplitude = ym_amplitude(setting[4] ? envelope_level :
                    (setting[3:0] == 0 ? 5'd0 : {setting[3:0], 1'b1}));
            else channel_amplitude = amplitude(setting[4] ? envelope_level[3:0] : setting[3:0]);
        end
    endfunction
    wire [2:0] gate = (tone | registers[7][2:0]) &
                     ({3{noise[0]}} | registers[7][5:3]);
    wire [7:0] a = gate[0] ? channel_amplitude(registers[8][4:0]) : 0;
    wire [7:0] b = gate[1] ? channel_amplitude(registers[9][4:0]) : 0;
    wire [7:0] c = gate[2] ? channel_amplitude(registers[10][4:0]) : 0;
    assign pcm = a + b + c;
    assign read_data = selected_valid ? registers[selected] : 8'hff;
endmodule
