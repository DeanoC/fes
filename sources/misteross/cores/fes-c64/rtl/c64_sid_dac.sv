// SPDX-License-Identifier: GPL-2.0-or-later
// MOS6581 ladder constants derived from pinned reSID build_dac_table.
// Copyright (C) 2010 Dag Lem <resid@nimrod.no>. Adapted diagnostic prototype.
module c64_sid_dac(input wire [11:0] wave, input wire [7:0] envelope,
 output wire [11:0] wave_dac, output wire [7:0] envelope_dac);
 wire [23:0] wave_sum = 24'd63923 +
  (wave[0] ? 24'd4200 : 24'd0) +
  (wave[1] ? 24'd6109 : 24'd0) +
  (wave[2] ? 24'd10795 : 24'd0) +
  (wave[3] ? 24'd20388 : 24'd0) +
  (wave[4] ? 24'd39248 : 24'd0) +
  (wave[5] ? 24'd75947 : 24'd0) +
  (wave[6] ? 24'd147168 : 24'd0) +
  (wave[7] ? 24'd285284 : 24'd0) +
  (wave[8] ? 24'd553074 : 24'd0) +
  (wave[9] ? 24'd1072261 : 24'd0) +
  (wave[10] ? 24'd2078840 : 24'd0) +
  (wave[11] ? 24'd4030347 : 24'd0);
 assign wave_dac=wave_sum[22:11];
 wire [16:0] envelope_sum = 17'd618 +
  (envelope[0] ? 17'd462 : 17'd0) +
  (envelope[1] ? 17'd672 : 17'd0) +
  (envelope[2] ? 17'd1187 : 17'd0) +
  (envelope[3] ? 17'd2242 : 17'd0) +
  (envelope[4] ? 17'd4316 : 17'd0) +
  (envelope[5] ? 17'd8352 : 17'd0) +
  (envelope[6] ? 17'd16185 : 17'd0) +
  (envelope[7] ? 17'd31374 : 17'd0);
 assign envelope_dac=envelope_sum[15:8];
endmodule
