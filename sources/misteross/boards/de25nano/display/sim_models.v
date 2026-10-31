// SPDX-License-Identifier: GPL-2.0-or-later
// Controllable digital boundaries only: no analog PLL, sector or I2C model.
module sys_pll(input wire refclk, rst, output reg outclk_0 = 0, output reg locked = 0);
endmodule
module agilex_reset_release(output reg ninit_done = 1);
endmodule
module I2C_HDMI_Config(input wire iCLK, iRST_N, HDMI_TX_INT,
                      output wire I2C_SCLK, inout wire I2C_SDAT, output wire READY);
    assign I2C_SCLK = 1'b1;
    assign I2C_SDAT = 1'bz;
    assign READY = iRST_N;
endmodule
