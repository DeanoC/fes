// SPDX-License-Identifier: GPL-2.0-or-later
module coleco_audio_mix (
    input wire signed [15:0] sn_sample,
    input wire signed [15:0] ay_sample,
    output wire signed [15:0] mixed_sample
);
    wire signed [16:0] sum = {sn_sample[15], sn_sample} +
                             {ay_sample[15], ay_sample};
    assign mixed_sample = sum > 17'sd32767 ? 16'sh7fff :
                          sum < -17'sd32768 ? 16'sh8000 : sum[15:0];
endmodule
