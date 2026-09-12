// SPDX-License-Identifier: GPL-2.0-or-later
// Independent-clock simple dual-port RAM for the captured Coleco raster.

module coleco_video_dpram #(
    parameter DATAWIDTH = 2,
    parameter ADDRWIDTH = 16,
    parameter NUMWORDS = 1 << ADDRWIDTH
) (
    input  wire                  clock_a,
    input  wire [ADDRWIDTH-1:0]  address_a,
    input  wire [DATAWIDTH-1:0]  data_a,
    input  wire                  wren_a,
    input  wire                  clock_b,
    input  wire [ADDRWIDTH-1:0]  address_b,
    output wire [DATAWIDTH-1:0]  q_b
);
`ifdef QUARTUS
    altsyncram altsyncram_component (
        .address_a(address_a),
        .address_b(address_b),
        .clock0(clock_a),
        .clock1(clock_b),
        .data_a(data_a),
        .data_b({DATAWIDTH{1'b0}}),
        .wren_a(wren_a),
        .wren_b(1'b0),
        .q_a(),
        .q_b(q_b),
        .aclr0(1'b0),
        .aclr1(1'b0),
        .addressstall_a(1'b0),
        .addressstall_b(1'b0),
        .byteena_a(1'b1),
        .byteena_b(1'b1),
        .clocken0(1'b1),
        .clocken1(1'b1),
        .clocken2(1'b1),
        .clocken3(1'b1),
        .eccstatus(),
        .rden_a(1'b0),
        .rden_b(1'b1)
    );
    defparam
        altsyncram_component.wrcontrol_wraddress_reg_b = "CLOCK1",
        altsyncram_component.address_reg_b = "CLOCK1",
        altsyncram_component.indata_reg_b = "CLOCK1",
        altsyncram_component.numwords_a = NUMWORDS,
        altsyncram_component.numwords_b = NUMWORDS,
        altsyncram_component.widthad_a = ADDRWIDTH,
        altsyncram_component.widthad_b = ADDRWIDTH,
        altsyncram_component.width_a = DATAWIDTH,
        altsyncram_component.width_b = DATAWIDTH,
        altsyncram_component.width_byteena_a = 1,
        altsyncram_component.width_byteena_b = 1,
        altsyncram_component.clock_enable_input_a = "BYPASS",
        altsyncram_component.clock_enable_input_b = "BYPASS",
        altsyncram_component.clock_enable_output_a = "BYPASS",
        altsyncram_component.clock_enable_output_b = "BYPASS",
        altsyncram_component.intended_device_family = "Cyclone V",
        altsyncram_component.lpm_type = "altsyncram",
        altsyncram_component.ram_block_type = "M10K",
        altsyncram_component.operation_mode = "BIDIR_DUAL_PORT",
        altsyncram_component.outdata_aclr_a = "NONE",
        altsyncram_component.outdata_aclr_b = "NONE",
        altsyncram_component.outdata_reg_a = "UNREGISTERED",
        altsyncram_component.outdata_reg_b = "CLOCK1",
        altsyncram_component.power_up_uninitialized = "FALSE",
        altsyncram_component.read_during_write_mode_mixed_ports = "DONT_CARE",
        altsyncram_component.read_during_write_mode_port_a = "NEW_DATA_NO_NBE_READ",
        altsyncram_component.read_during_write_mode_port_b = "NEW_DATA_NO_NBE_READ";
`else
    // This is the same independent-clock inference shape as Misteross
    // experiment 490_m10k_sdp40. The read result is intentionally registered
    // on clock_b so Yosys/nextpnr can map the table to MISTRAL_M10K.
    (* ramstyle = "M10K" *) reg [DATAWIDTH-1:0] ram [0:NUMWORDS-1];
    reg [DATAWIDTH-1:0] q_b_r;

    always @(posedge clock_a) begin
        if (wren_a)
            ram[address_a] <= data_a;
    end

    always @(posedge clock_b)
        q_b_r <= ram[address_b];

    assign q_b = q_b_r;
`endif
endmodule
