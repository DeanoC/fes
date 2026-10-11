// SPDX-License-Identifier: GPL-3.0-or-later
// ST even-byte YM2149 address/read and data-write registers. Clock and
// sample enables stay in the transport clock domain. Nonlinear table output
// is unipolar; filtering and weighted downsampling remain unimplemented.
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
    // Submit the updated tone/noise/envelope levels one fabric edge after
    // each /8 engine counter tick. The bounded mixer completes before the
    // next counter tick on the production 52.224 MHz board clock.
    reg [2:0] counter_phase;
    reg mix_req;
    wire mix_busy, mix_ready;
    wire [14:0] mixed_pcm, unused_result_levels;
    st_ym_mixer mixer (.clk(clk), .reset(reset), .req(mix_req), .levels(ym_levels),
        .busy(mix_busy), .ready(mix_ready), .pcm(mixed_pcm), .result_levels(unused_result_levels));
    wire unused_mix_status = mix_busy ^ mix_ready;
    always @(posedge clk) begin
        if(reset)begin counter_phase<=0;mix_req<=0;end
        else begin
            mix_req<=chip_ce && counter_phase==3'd7;
            if(chip_ce)counter_phase<=counter_phase+3'd1;
        end
    end
    always @(posedge clk) begin
        if (reset) begin
            chip_phase <= 0; sample_phase <= 0; pcm_signed <= 0; sample_valid <= 0;
            bus_ack <= 0; bus_rdata <= 0; selected <= 0; selected_valid <= 1;
            port_direction <= 0; port_a_latch <= 0; port_b_latch <= 0;
        end else begin
            chip_phase <= chip_ce ? 32'(chip_sum - 33'(SYSTEM_CLOCK_HZ)) : chip_sum[31:0];
            sample_phase <= sample_ce ? 32'(sample_sum - 33'(SYSTEM_CLOCK_HZ)) : sample_sum[31:0];
            sample_valid <= sample_ce;
            if (sample_ce) pcm_signed <= $signed({1'b0,mixed_pcm});
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
