"""Connect selected board logic to actual parent reset/transport logic.

Only PLL clock/lock and HDMI configuration completion are boundary models.
Memory storage uses the separately guarded explicit M20K model. This is a
finite digital composition test, not physical, SMMU or boot-epoch acceptance.
"""
CASES = ["composed_reset_disabled"] + [f"queued_phase_{phase}_stop_{mode}"
          for phase in (1, 17, 296) for mode in range(4)]
SOURCES = ("fes_mailbox/fes_de25_scanout_board.v", "fes_mailbox/fes_de25_top_reset.v", "fes_mailbox/clocks.sdc")
IO_MODEL = '''module sys_pll(input wire refclk, rst, output wire outclk_0, locked);
 assign outclk_0=$root.fes_de25_scanout_interconnect_test.clk_pixel;
 assign locked=$root.fes_de25_scanout_interconnect_test.model_pll_locked && !rst;
endmodule
module fes_de25_scanout_hdmi_config(input wire iCLK, iRST_N, HDMI_TX_INT, I2C_SDAT_IN,
 output wire I2C_SCLK, inout wire I2C_SDAT, output wire READY);
 assign I2C_SCLK=1'b1; assign I2C_SDAT=1'bz; assign READY=iRST_N;
endmodule
'''


def wrapper(source):
    before = "input wire clk_mem, clk_pixel, reset_n, clock_fault, display_ready, scanout_source_n,"
    after = """input wire clk_mem, clk_pixel, reset_n, clk_ref, model_pll_locked, model_ninit_done,
 output wire clock_fault, display_ready, scanout_source_n,
 output wire board_pixel_clk, hdmi_de, hdmi_hs, hdmi_vs,
 output wire [23:0] hdmi_rgb,"""
    if source.count(before) != 1 or source.count("endmodule") != 1:
        raise ValueError("board composition requires the actual-parent-reset transport wrapper")
    instance = '''
tri1 hdmi_sda;
fes_de25_scanout_board board(.CLOCK0_50(clk_ref), .system_clk(clk_mem), .top_reset_n(reset_n),
 .ninit_done(model_ninit_done), .HDMI_TX_INT(1'b1), .video_rgb(video_rgb), .video_de(video_de), .video_hs(video_hs), .video_vs(video_vs),
 .pixel_clk(board_pixel_clk), .scanout_reset_n(scanout_source_n), .clock_fault(clock_fault), .display_ready(display_ready),
 .HDMI_TX_CLK(), .HDMI_TX_DE(hdmi_de), .HDMI_TX_HS(hdmi_hs), .HDMI_TX_VS(hdmi_vs), .HDMI_TX_D(hdmi_rgb),
 .HDMI_I2C_SCL(), .HDMI_I2C_SDA(hdmi_sda), .HDMI_MCLK(), .HDMI_SCLK(), .HDMI_LRCLK(), .HDMI_I2S());
'''
    return source.replace(before, after).replace("endmodule", instance + "\nendmodule")
