// Two dual-clock 512x20 SDP M10Ks. Both CLK ports are live so the
// selector audit can require CLKIN.0/.1 and unique sites.
module top #(
    parameter [15:0] SIGNATURE = 16'hD860
) (
    input wire FPGA_CLK1_50
);
    function automatic [10239:0] packed_init;
        input integer bank;
        integer address;
        begin
            packed_init = 10240'b0;
            for (address = 0; address < 512; address = address + 1)
                packed_init[address * 20 +: 20] =
                    (address[19:0] * 20'd73) ^ (address[19:0] >> 1) ^ 20'h00A6
                    ^ ((bank != 0) ? 20'h0011 : 20'h0000);
        end
    endfunction

    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire [19:0] q0;
    wire [19:0] q1;
    wire [8:0] addr;
    wire bank;
    reg armed = 1'b0;
    reg we0 = 1'b0;
    reg we1 = 1'b0;
    reg [8:0] waddr = 9'd0;
    reg [19:0] wdata = 20'd0;
    reg [15:0] sampled = 16'd0;

    assign addr = gp_out[8:0];
    assign bank = gp_out[9];

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        waddr <= addr;
        wdata <= gp_out[29:10];
        we0 <= armed && gp_out[31] && !bank;
        we1 <= armed && gp_out[31] && bank;
        sampled <= (bank ? q1[15:0] : q0[15:0]);
    end

    (* keep *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1),
        .INIT(packed_init(0))
    ) mem0 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(waddr),
        .A1DATA(wdata),
        .A1EN(we0),
        .A1BE(2'b11),
        .B1ADDR(addr),
        .B1DATA(q0),
        .B1EN(1'b1),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    (* keep *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1),
        .INIT(packed_init(1))
    ) mem1 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(waddr),
        .A1DATA(wdata),
        .A1EN(we1),
        .A1BE(2'b11),
        .B1ADDR(addr),
        .B1DATA(q1),
        .B1EN(1'b1),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, sampled};
endmodule
