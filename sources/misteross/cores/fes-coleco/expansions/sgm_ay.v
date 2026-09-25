// SPDX-License-Identifier: GPL-2.0-or-later
// AY-3-8910 compatible three-channel PSG for Opcode SGM.
// See GI AY-3-8910 datasheet and MAME ay8910 tone/noise/envelope timing.
// All state runs on the Coleco system clock; enables model the 1.7897725 MHz
// chip clock and its /8 tone/noise/envelope update rate.
module sgm_ay (
    input wire clk,
    input wire reset,
    input wire address_write,
    input wire data_write,
    input wire data_read,
    input wire [7:0] write_data,
    output wire [7:0] read_data,
    output reg signed [15:0] sample_signed = 0
);
    reg [7:0] registers [0:15];
    reg [3:0] selected = 0;
    assign read_data = data_read ? registers[selected] : 8'h00;

    reg [26:0] phase = 0;
    wire [27:0] phase_next = {1'b0, phase} + 28'd3579545;
    wire chip_ce = phase_next >= 28'd104448000;
    reg [2:0] prescale = 0;
    wire step_ce = chip_ce && prescale == 3'd7;

    reg [11:0] tone_count [0:2];
    reg [2:0] tone_out = 0;
    reg [4:0] noise_count = 0;
    reg noise_prescale = 0;
    reg [16:0] noise_rng = 17'h00001;
    reg [16:0] envelope_count = 0;
    reg [3:0] envelope_step = 0;
    reg [3:0] envelope_attack = 0;
    reg envelope_hold = 0;
    reg envelope_alternate = 0;
    reg envelope_holding = 0;
    wire [3:0] envelope_level = envelope_step ^ envelope_attack;
    wire [15:0] envelope_period = {registers[12], registers[11]};
    wire [16:0] envelope_limit = envelope_period == 0 ? 17'd2 :
                                  {envelope_period, 1'b0};

    function automatic [11:0] tone_period(input [7:0] fine, input [7:0] coarse);
        reg [11:0] combined;
        begin
            combined = {coarse[3:0], fine};
            tone_period = combined == 0 ? 12'd1 : combined;
        end
    endfunction
    function automatic [4:0] noise_period(input [7:0] value);
        noise_period = value[4:0] == 0 ? 5'd1 : value[4:0];
    endfunction
    function automatic signed [15:0] amplitude(input [3:0] volume);
        begin
            case (volume)
                0: amplitude=0;       1: amplitude=70;
                2: amplitude=100;     3: amplitude=142;
                4: amplitude=201;     5: amplitude=284;
                6: amplitude=402;     7: amplitude=568;
                8: amplitude=804;     9: amplitude=1137;
                10: amplitude=1608;   11: amplitude=2274;
                12: amplitude=3216;   13: amplitude=4548;
                14: amplitude=6432;   default: amplitude=8191;
            endcase
        end
    endfunction
    function automatic signed [15:0] channel_pcm(
        input [3:0] volume,
        input tone_level, input noise_level,
        input tone_disabled, input noise_disabled
    );
        reg signed [15:0] magnitude;
        begin
            magnitude = amplitude(volume);
            channel_pcm = ((tone_level || tone_disabled) &&
                           (noise_level || noise_disabled)) ? magnitude : -magnitude;
        end
    endfunction

    wire [3:0] volume_a = registers[8][4] ? envelope_level : registers[8][3:0];
    wire [3:0] volume_b = registers[9][4] ? envelope_level : registers[9][3:0];
    wire [3:0] volume_c = registers[10][4] ? envelope_level : registers[10][3:0];
    wire signed [15:0] pcm_a = channel_pcm(volume_a, tone_out[0], noise_rng[0],
                                            registers[7][0], registers[7][3]);
    wire signed [15:0] pcm_b = channel_pcm(volume_b, tone_out[1], noise_rng[0],
                                            registers[7][1], registers[7][4]);
    wire signed [15:0] pcm_c = channel_pcm(volume_c, tone_out[2], noise_rng[0],
                                            registers[7][2], registers[7][5]);
    wire [11:0] period_a = tone_period(registers[0], registers[1]);
    wire [11:0] period_b = tone_period(registers[2], registers[3]);
    wire [11:0] period_c = tone_period(registers[4], registers[5]);
    integer i;
    initial begin
        for (i=0; i<16; i=i+1) registers[i] = 0;
        for (i=0; i<3; i=i+1) tone_count[i] = 0;
    end
    always @(posedge clk) begin
        if (reset) begin
            for (i=0; i<16; i=i+1) registers[i] <= 0;
            for (i=0; i<3; i=i+1) tone_count[i] <= 0;
            selected <= 0;
            phase <= 0;
            prescale <= 0;
            tone_out <= 0;
            noise_count <= 0;
            noise_prescale <= 0;
            noise_rng <= 17'h00001;
            envelope_count <= 0;
            envelope_step <= 0;
            envelope_attack <= 0;
            envelope_hold <= 0;
            envelope_alternate <= 0;
            envelope_holding <= 0;
            sample_signed <= 0;
        end else begin
            phase <= chip_ce ? 27'(phase_next - 28'd104448000) : phase_next[26:0];
            if (chip_ce) prescale <= prescale + 1'b1;
            sample_signed <= pcm_a + pcm_b + pcm_c;
            if (address_write) selected <= write_data[3:0];
            if (data_write) begin
                case (selected)
                    1, 3, 5: registers[selected] <= {4'b0, write_data[3:0]};
                    6: registers[selected] <= {3'b0, write_data[4:0]};
                    8, 9, 10: registers[selected] <= {3'b0, write_data[4:0]};
                    13: registers[selected] <= {4'b0, write_data[3:0]};
                    default: registers[selected] <= write_data;
                endcase
                if (selected == 13) begin
                    envelope_count <= 0;
                    envelope_step <= 4'd15;
                    envelope_attack <= write_data[2] ? 4'hf : 4'h0;
                    envelope_hold <= !write_data[3] || write_data[0];
                    envelope_alternate <= !write_data[3] ? write_data[2] : write_data[1];
                    envelope_holding <= 0;
                end
            end
            if (step_ce) begin
                if (tone_count[0] + 1'b1 >= period_a) begin
                    tone_count[0] <= 0; tone_out[0] <= ~tone_out[0];
                end else tone_count[0] <= tone_count[0] + 1'b1;
                if (tone_count[1] + 1'b1 >= period_b) begin
                    tone_count[1] <= 0; tone_out[1] <= ~tone_out[1];
                end else tone_count[1] <= tone_count[1] + 1'b1;
                if (tone_count[2] + 1'b1 >= period_c) begin
                    tone_count[2] <= 0; tone_out[2] <= ~tone_out[2];
                end else tone_count[2] <= tone_count[2] + 1'b1;
                if (noise_count + 1'b1 >= noise_period(registers[6])) begin
                    noise_count <= 0;
                    noise_prescale <= ~noise_prescale;
                    if (noise_prescale)
                        noise_rng <= {noise_rng[0] ^ noise_rng[3], noise_rng[16:1]};
                end else noise_count <= noise_count + 1'b1;
                if (!envelope_holding && !(data_write && selected == 13)) begin
                    if (envelope_count + 1'b1 >= envelope_limit) begin
                        envelope_count <= 0;
                        if (envelope_step != 0)
                            envelope_step <= envelope_step - 1'b1;
                        else if (envelope_hold) begin
                            envelope_holding <= 1;
                            if (envelope_alternate) envelope_attack <= ~envelope_attack;
                        end else begin
                            envelope_step <= 4'd15;
                            if (envelope_alternate) envelope_attack <= ~envelope_attack;
                        end
                    end else envelope_count <= envelope_count + 1'b1;
                end
            end
        end
    end
endmodule
