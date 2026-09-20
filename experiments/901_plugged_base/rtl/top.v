// Empty-socket shell. GPI 0xD901. Locked plug FFs sit outside the
// reserved rect (LAB columns 24 and 28); the socket interior is vacant
// until a cart is merged. nextpnr-mistral only accepts the even FF in
// each ALM half (z%6 is 2 or 4).
module top #(
    parameter [15:0] SIGNATURE = 16'hD901
) (
    input wire FPGA_CLK1_50
);
    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire [15:0] plug_addr;
    wire [15:0] plug_addr_d;
    wire [9:0] plug_rdata;
    (* keep *) wire [9:0] plug_rdata_d;

    assign plug_addr_d = gp_out[15:0];
    assign plug_rdata_d = 10'd0;

    (* keep, BEL = "MISTRAL_FF.24.1.2" *)
    MISTRAL_FF plug_addr_ff_0 (
        .DATAIN(plug_addr_d[0]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[0])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.4" *)
    MISTRAL_FF plug_addr_ff_1 (
        .DATAIN(plug_addr_d[1]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[1])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.8" *)
    MISTRAL_FF plug_addr_ff_2 (
        .DATAIN(plug_addr_d[2]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[2])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.10" *)
    MISTRAL_FF plug_addr_ff_3 (
        .DATAIN(plug_addr_d[3]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[3])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.14" *)
    MISTRAL_FF plug_addr_ff_4 (
        .DATAIN(plug_addr_d[4]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[4])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.16" *)
    MISTRAL_FF plug_addr_ff_5 (
        .DATAIN(plug_addr_d[5]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[5])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.20" *)
    MISTRAL_FF plug_addr_ff_6 (
        .DATAIN(plug_addr_d[6]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[6])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.22" *)
    MISTRAL_FF plug_addr_ff_7 (
        .DATAIN(plug_addr_d[7]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[7])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.26" *)
    MISTRAL_FF plug_addr_ff_8 (
        .DATAIN(plug_addr_d[8]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[8])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.28" *)
    MISTRAL_FF plug_addr_ff_9 (
        .DATAIN(plug_addr_d[9]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[9])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.32" *)
    MISTRAL_FF plug_addr_ff_10 (
        .DATAIN(plug_addr_d[10]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[10])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.34" *)
    MISTRAL_FF plug_addr_ff_11 (
        .DATAIN(plug_addr_d[11]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[11])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.38" *)
    MISTRAL_FF plug_addr_ff_12 (
        .DATAIN(plug_addr_d[12]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[12])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.40" *)
    MISTRAL_FF plug_addr_ff_13 (
        .DATAIN(plug_addr_d[13]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[13])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.44" *)
    MISTRAL_FF plug_addr_ff_14 (
        .DATAIN(plug_addr_d[14]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[14])
    );
    (* keep, BEL = "MISTRAL_FF.24.1.46" *)
    MISTRAL_FF plug_addr_ff_15 (
        .DATAIN(plug_addr_d[15]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_addr[15])
    );
    (* keep, BEL = "MISTRAL_FF.28.1.2" *)
    MISTRAL_FF plug_rdata_ff_0 (
        .DATAIN(plug_rdata_d[0]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[0])
    );
    (* keep, BEL = "MISTRAL_FF.28.2.2" *)
    MISTRAL_FF plug_rdata_ff_1 (
        .DATAIN(plug_rdata_d[1]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[1])
    );
    (* keep, BEL = "MISTRAL_FF.28.3.2" *)
    MISTRAL_FF plug_rdata_ff_2 (
        .DATAIN(plug_rdata_d[2]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[2])
    );
    (* keep, BEL = "MISTRAL_FF.28.4.2" *)
    MISTRAL_FF plug_rdata_ff_3 (
        .DATAIN(plug_rdata_d[3]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[3])
    );
    (* keep, BEL = "MISTRAL_FF.28.5.2" *)
    MISTRAL_FF plug_rdata_ff_4 (
        .DATAIN(plug_rdata_d[4]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[4])
    );
    (* keep, BEL = "MISTRAL_FF.28.6.2" *)
    MISTRAL_FF plug_rdata_ff_5 (
        .DATAIN(plug_rdata_d[5]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[5])
    );
    (* keep, BEL = "MISTRAL_FF.28.7.2" *)
    MISTRAL_FF plug_rdata_ff_6 (
        .DATAIN(plug_rdata_d[6]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[6])
    );
    (* keep, BEL = "MISTRAL_FF.28.8.2" *)
    MISTRAL_FF plug_rdata_ff_7 (
        .DATAIN(plug_rdata_d[7]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[7])
    );
    (* keep, BEL = "MISTRAL_FF.28.9.2" *)
    MISTRAL_FF plug_rdata_ff_8 (
        .DATAIN(plug_rdata_d[8]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[8])
    );
    (* keep, BEL = "MISTRAL_FF.28.10.2" *)
    MISTRAL_FF plug_rdata_ff_9 (
        .DATAIN(plug_rdata_d[9]), .CLK(FPGA_CLK1_50), .ACLR(1'b1), .ENA(1'b1),
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0), .Q(plug_rdata[9])
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    // Bits [15:10] observe plug_addr so a vacant shell can prove the GPO
    // path; cart rdata stays in [9:0].
    assign gp_in = {SIGNATURE, plug_addr[5:0], plug_rdata};
endmodule
