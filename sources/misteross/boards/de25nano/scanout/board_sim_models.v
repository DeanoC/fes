// Digital configuration-completion boundary only. Actual I2C protocol is
// exercised separately using the retained, derived vendor controller.
module fes_de25_scanout_hdmi_config(input wire iCLK, iRST_N, HDMI_TX_INT, I2C_SDAT_IN,
        output wire I2C_SCLK, inout wire I2C_SDAT, output wire READY);
    assign I2C_SCLK = 1'b1;
    assign I2C_SDAT = 1'bz;
    reg configured = 1'b1;
    assign READY = iRST_N && configured;
endmodule
