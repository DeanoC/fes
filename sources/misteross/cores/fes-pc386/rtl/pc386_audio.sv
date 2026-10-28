// SPDX-License-Identifier: MIT
// pc386 audio (docs/pc386-machine-design.md, "Audio"): the Sound Blaster 2.0
// DSP and the OPL2 behind the AT core's sound port, and the mixer that adds
// the PC speaker and resamples all three to 48 kHz mono.
//
// The AT core decodes the sound port: the Sound Blaster's 16 ports at the
// latched base (adlib = 0, address = port[3:0]) and 388h-389h (adlib = 1).
// The OPL2 answers at base+8-9 and at 388h-389h; the DSP at the rest of the
// base range. The DSP's IRQ drives the latched line: 2 (the AT's IRQ 9), 3,
// 5 or 7. DMA is channel 1, the only choice an SB 2.0 has.
module pc386_audio #(
    parameter integer US_N = 1, US_D = 100,            // 1 MHz DSP time base
    parameter integer OPL2_N = 7, OPL2_D = 14080,      // 3.579545 MHz / 72
    parameter integer DSP_RESET_US = 20
) (
    input wire clk, reset,
    // Sound port from the AT core
    input wire access, write, adlib,
    input wire [3:0] address,
    input wire [7:0] wdata,
    output wire [7:0] rdata,
    input wire [7:0] sb_irq,
    output wire [15:0] irq,
    // DMA channel 1
    output wire drq1,
    input wire dack_valid, dack_write,
    input wire [7:0] dack_wdata,
    output wire dack_done,
    output wire [7:0] dack_rdata,
    // Mixer
    input wire speaker,
    input wire request,                                // 48 kHz frame request
    output wire sample_valid,
    output wire signed [15:0] sample,
    // Sources before the mixer, for the checks
    output wire opl2_valid,
    output wire signed [15:0] opl2_sample,
    output wire [7:0] dsp_dac
);
    wire select_opl2 = adlib || address[3:1] == 3'd4;

    wire us_tick;
    pc386_rate #(.N(US_N), .D(US_D)) us_rate (.clk(clk), .reset(reset), .tick(us_tick));

    wire [7:0] opl2_rdata;
    pc386_opl2 #(.SAMPLE_N(OPL2_N), .SAMPLE_D(OPL2_D)) opl2 (
        .clk(clk), .reset(reset), .access(access && select_opl2), .write(write), .a0(address[0]),
        .wdata(wdata), .rdata(opl2_rdata), .sample_valid(opl2_valid), .sample(opl2_sample)
    );

    wire [7:0] dsp_rdata;
    wire dsp_irq;
    pc386_sb #(.RESET_US(DSP_RESET_US)) dsp (
        .clk(clk), .reset(reset), .us_tick(us_tick),
        .access(access && !select_opl2), .write(write), .address(address), .wdata(wdata), .rdata(dsp_rdata),
        .irq(dsp_irq),
        .drq(drq1), .dack_valid(dack_valid), .dack_write(dack_write), .dack_wdata(dack_wdata),
        .dack_done(dack_done), .dack_rdata(dack_rdata), .dac(dsp_dac)
    );
    assign rdata = select_opl2 ? opl2_rdata : dsp_rdata;
    assign irq = {6'b0, dsp_irq && sb_irq == 8'd2, 1'b0, dsp_irq && sb_irq == 8'd7, 1'b0,
                  dsp_irq && sb_irq == 8'd5, 1'b0, dsp_irq && sb_irq == 8'd3, 3'b000};

    // The OPL2 holds each sample until the next; the DAC byte is unsigned.
    reg signed [15:0] opl2_held;
    always @(posedge clk)
        if (reset) opl2_held <= 0;
        else if (opl2_valid) opl2_held <= opl2_sample;
    pc386_mixer mixer (
        .clk(clk), .reset(reset), .request(request),
        .opl2(opl2_held), .dsp({dsp_dac ^ 8'h80, 8'h00}), .speaker(speaker),
        .sample_valid(sample_valid), .sample(sample)
    );
endmodule
