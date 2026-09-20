// SPDX-License-Identifier: GPL-2.0-or-later
// TI SN76489: three tones, 15-bit noise, 2 dB attenuation, mono signed PCM.
// ce is one pulse per chip clock; write is a single system-clock byte strobe.
// See Texas Instruments SN76489AN data sheet (1980), register/clock tables.
module fes_sn76489 (
    input wire clk, reset, ce, write,
    input wire [7:0] data,
    output reg signed [15:0] sample = 0
);
    reg [9:0] period [0:2];
    reg [3:0] attenuation [0:3];
    reg [9:0] count [0:2];
    reg [2:0] tone = 0;
    reg [3:0] divider = 0;
    reg [2:0] latched = 0, noise_control = 0;
    reg [6:0] noise_count = 0;
    reg [14:0] noise = 15'h4000;
    wire [2:0] selected = data[7] ? data[6:4] : latched;
    wire noise_write = write && selected == 6;
    wire tone2_rise = divider == 0 && count[2] <= 1 && !tone[2];
    // Fixed noise shift rates are chip clock / 512, /1024 and /2048.
    // Rate 3 follows the rising edge of tone 2, not its half-period.
    wire noise_shift = noise_control[1:0] == 3 ? tone2_rise :
                      divider == 0 && noise_count == 0;
    function automatic signed [15:0] amplitude(input bit level, input [3:0] atten);
        reg signed [15:0] magnitude;
        begin
            case (atten)
                0: magnitude=8191; 1: magnitude=6507; 2: magnitude=5168;
                3: magnitude=4105; 4: magnitude=3261; 5: magnitude=2590;
                6: magnitude=2057; 7: magnitude=1634; 8: magnitude=1298;
                9: magnitude=1031; 10: magnitude=819; 11: magnitude=650;
                12: magnitude=516; 13: magnitude=410; 14: magnitude=326;
                default: magnitude=0;
            endcase
            amplitude = level ? magnitude : -magnitude;
        end
    endfunction
    integer i;
    initial begin
        for (i=0; i<3; i=i+1) begin period[i]=0; count[i]=0; end
        for (i=0; i<4; i=i+1) attenuation[i]=15;
    end
    always @(posedge clk) begin
        if (reset) begin
            for (i=0; i<3; i=i+1) begin period[i]<=0; count[i]<=0; end
            for (i=0; i<4; i=i+1) attenuation[i]<=15;
            tone<=0; divider<=0; latched<=0; noise_control<=0;
            noise_count<=0; noise<=15'h4000; sample<=0;
        end else begin
            // Four full-scale channels sum to +/-32764 without clipping.
            sample <= amplitude(tone[0], attenuation[0]) +
                      amplitude(tone[1], attenuation[1]) +
                      amplitude(tone[2], attenuation[2]) +
                      amplitude(noise[0], attenuation[3]);
            if (write) begin
                if (data[7]) latched <= data[6:4];
                if (selected[0]) attenuation[selected[2:1]] <= data[3:0];
                else if (selected != 6) begin
                    if (data[7]) period[selected[2:1]][3:0] <= data[3:0];
                    else period[selected[2:1]][9:4] <= data[5:0];
                end
            end
            if (ce) begin
                divider <= divider + 1'b1;
                if (divider == 0) begin
                    for (i=0; i<3; i=i+1) begin
                        if (count[i] <= 1) begin
                            count[i] <= period[i] == 0 ? 10'd1 : period[i];
                            tone[i] <= ~tone[i];
                        end else count[i] <= count[i] - 1'b1;
                    end
                    if (noise_count == 0)
                        case (noise_control[1:0])
                            0: noise_count <= 31;
                            1: noise_count <= 63;
                            default: noise_count <= 127;
                        endcase
                    else noise_count <= noise_count - 1'b1;
                end
                if (noise_shift)
                    noise <= {noise[0] ^ (noise_control[2] && noise[1]), noise[14:1]};
            end
            // A noise write restarts the generator even on a shift edge.
            if (noise_write) begin
                noise_control <= data[2:0]; noise <= 15'h4000; noise_count <= 0;
            end
        end
    end
endmodule
