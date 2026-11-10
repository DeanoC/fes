// The P3 FES_VENDOR_RAM form: altsyncram BIDIR_DUAL_PORT with NEW_DATA_NO_NBE_READ, as
// coleco_video_dpram.v's QUARTUS branch, but with both outputs unregistered so that each port has
// the one registered read stage (the address register) the inferred form has.
module tdp_ram #(parameter int DW = 4, parameter int AW = 16, parameter int WORDS = 1 << AW) (
    input logic clk_a, input logic we_a, input logic [AW-1:0] addr_a, input logic [DW-1:0] wdata_a, output logic [DW-1:0] rdata_a,
    input logic clk_b, input logic we_b, input logic [AW-1:0] addr_b, input logic [DW-1:0] wdata_b, output logic [DW-1:0] rdata_b);
    altsyncram #(
        .operation_mode("BIDIR_DUAL_PORT"), .ram_block_type("M10K"), .intended_device_family("Cyclone V"),
        .lpm_type("altsyncram"), .power_up_uninitialized("FALSE"),
        .width_a(DW), .width_b(DW), .widthad_a(AW), .widthad_b(AW), .numwords_a(WORDS), .numwords_b(WORDS),
        .width_byteena_a(1), .width_byteena_b(1),
        .address_reg_b("CLOCK1"), .indata_reg_b("CLOCK1"), .wrcontrol_wraddress_reg_b("CLOCK1"),
        .outdata_reg_a("UNREGISTERED"), .outdata_reg_b("UNREGISTERED"),
        .outdata_aclr_a("NONE"), .outdata_aclr_b("NONE"),
        .clock_enable_input_a("BYPASS"), .clock_enable_input_b("BYPASS"),
        .clock_enable_output_a("BYPASS"), .clock_enable_output_b("BYPASS"),
        .read_during_write_mode_mixed_ports("DONT_CARE"),
        .read_during_write_mode_port_a("NEW_DATA_NO_NBE_READ"), .read_during_write_mode_port_b("NEW_DATA_NO_NBE_READ")
    ) ram (
        .clock0(clk_a), .clock1(clk_b), .address_a(addr_a), .address_b(addr_b), .data_a(wdata_a), .data_b(wdata_b),
        .wren_a(we_a), .wren_b(we_b), .q_a(rdata_a), .q_b(rdata_b),
        .aclr0(1'b0), .aclr1(1'b0), .addressstall_a(1'b0), .addressstall_b(1'b0), .byteena_a(1'b1), .byteena_b(1'b1),
        .clocken0(1'b1), .clocken1(1'b1), .clocken2(1'b1), .clocken3(1'b1), .eccstatus(), .rden_a(1'b1), .rden_b(1'b1));
endmodule
