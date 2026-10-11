// SPDX-License-Identifier: GPL-3.0-or-later
// ST even-byte YM2149 address/read and data-write registers. Clock and
// sample enables stay in the transport clock domain. The DAC approximation
// is unipolar, with silence at zero and headroom for all three channels.
module st_ym2149 #(
    parameter integer SYSTEM_CLOCK_HZ = 52_224_000,
    parameter integer CHIP_CLOCK_HZ = 2_000_000,
    parameter integer SAMPLE_RATE_HZ = 48_000
) (
    input wire clk, reset,
    input wire bus_req, bus_reg, bus_write,
    input wire [7:0] bus_wdata,
    output reg [7:0] bus_rdata,
    output reg bus_ack,
    output reg signed [15:0] pcm_signed,
    output reg sample_valid,
    // Port A bit 0 is floppy side, bits 1/2 are active-low drive selects.
    // Unconnected inputs are pulled high; pin output is high in input mode.
    output wire [7:0] port_a
);
    reg [31:0] chip_phase, sample_phase;
    wire [32:0] chip_sum = {1'b0, chip_phase} + 33'(CHIP_CLOCK_HZ);
    wire [32:0] sample_sum = {1'b0, sample_phase} + 33'(SAMPLE_RATE_HZ);
    wire chip_ce = chip_sum >= 33'(SYSTEM_CLOCK_HZ);
    wire sample_ce = sample_sum >= 33'(SYSTEM_CLOCK_HZ);
    wire bus_first = bus_req && !bus_ack;
    reg [3:0] selected;
    reg selected_valid;
    reg [1:0] port_direction;
    reg [7:0] port_a_latch, port_b_latch;
    wire [7:0] chip_read, unused_coarse_pcm;
    wire [14:0] ym_levels;
    wire [7:0] selected_read = selected_valid && selected == 14 ? port_a :
                               selected_valid && selected == 15 ?
                               (port_direction[1] ? port_b_latch : 8'hff) : chip_read;
    assign port_a = port_direction[0] ? port_a_latch : 8'hff;
    // Preserve the existing AY8912 card's default model; the YM option
    // supplies both I/O latches and a 32-step, /8 envelope in one engine.
    zonx_ay #(.YM2149(1)) sound (
        .clk(clk), .reset_n(!reset), .chip_ce(chip_ce),
        .address_write(bus_first && bus_write && !bus_reg),
        .data_write(bus_first && bus_write && bus_reg), .data(bus_wdata),
        .read_data(chip_read), .pcm(unused_coarse_pcm), .ym_levels(ym_levels)
    );
    // Single-channel STF levels expanded from measured 4-bit levels from Hatari v2.5.0 sound.c
    // ymout1c5bit. Scale 65535 to 10922 with nearest rounding so three
    // maximum voices fit signed PCM. Mixing remains a linear approximation;
    // preserving 32 levels does not claim analogue/nonlinear equivalence.
    // https://github.com/hatari/hatari/blob/v2.5.0/src/sound.c
    function automatic [13:0] dac_level(input [4:0] level);
        begin
            case (level)
                5'd0: dac_level = 14'd0;
                5'd1: dac_level = 14'd61;
                5'd2: dac_level = 14'd73;
                5'd3: dac_level = 14'd87;
                5'd4: dac_level = 14'd103;
                5'd5: dac_level = 14'd122;
                5'd6: dac_level = 14'd146;
                5'd7: dac_level = 14'd173;
                5'd8: dac_level = 14'd206;
                5'd9: dac_level = 14'd244;
                5'd10: dac_level = 14'd291;
                5'd11: dac_level = 14'd345;
                5'd12: dac_level = 14'd410;
                5'd13: dac_level = 14'd488;
                5'd14: dac_level = 14'd580;
                5'd15: dac_level = 14'd689;
                5'd16: dac_level = 14'd819;
                5'd17: dac_level = 14'd973;
                5'd18: dac_level = 14'd1157;
                5'd19: dac_level = 14'd1375;
                5'd20: dac_level = 14'd1634;
                5'd21: dac_level = 14'd1942;
                5'd22: dac_level = 14'd2308;
                5'd23: dac_level = 14'd2744;
                5'd24: dac_level = 14'd3261;
                5'd25: dac_level = 14'd3875;
                5'd26: dac_level = 14'd4606;
                5'd27: dac_level = 14'd5474;
                5'd28: dac_level = 14'd6506;
                5'd29: dac_level = 14'd7732;
                5'd30: dac_level = 14'd9190;
                5'd31: dac_level = 14'd10922;
            endcase
        end
    endfunction
    wire [15:0] mixed_pcm = {2'd0, dac_level(ym_levels[4:0])} +
                           {2'd0, dac_level(ym_levels[9:5])} +
                           {2'd0, dac_level(ym_levels[14:10])};
    always @(posedge clk) begin
        if (reset) begin
            chip_phase <= 0; sample_phase <= 0; pcm_signed <= 0; sample_valid <= 0;
            bus_ack <= 0; bus_rdata <= 0; selected <= 0; selected_valid <= 1;
            port_direction <= 0; port_a_latch <= 0; port_b_latch <= 0;
        end else begin
            chip_phase <= chip_ce ? 32'(chip_sum - 33'(SYSTEM_CLOCK_HZ)) : chip_sum[31:0];
            sample_phase <= sample_ce ? 32'(sample_sum - 33'(SYSTEM_CLOCK_HZ)) : sample_sum[31:0];
            sample_valid <= sample_ce;
            if (sample_ce) pcm_signed <= $signed(mixed_pcm);
            if (!bus_req) bus_ack <= 0;
            if (bus_first) begin
                bus_ack <= 1;
                // Only FF8800 reads the selected register. FF8802 is write-only.
                bus_rdata <= bus_reg ? 8'hff : selected_read;
                if (bus_write && !bus_reg) begin
                    selected <= bus_wdata[3:0]; selected_valid <= bus_wdata[7:4] == 0;
                end else if (bus_write && selected_valid) begin
                    case (selected)
                        7: port_direction <= bus_wdata[7:6];
                        14: port_a_latch <= bus_wdata;
                        15: port_b_latch <= bus_wdata;
                        default: ;
                    endcase
                end
            end
        end
    end
endmodule
