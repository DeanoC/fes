// 512x20 SDP M10K with a registered B-port read. Locked Yosys has no
// output-register parameter, so OSS sets CFG_OUT_REG_B after synthesis.
module top #(
    parameter [15:0] SIGNATURE = 16'hD42D
) (
    input wire FPGA_CLK1_50
);
    function automatic [10239:0] packed_init;
        integer address;
        begin
            packed_init = 10240'b0;
            for (address = 0; address < 512; address = address + 1)
                packed_init[address * 20 +: 20] = (address[19:0] * 20'd73) ^ (address[19:0] >> 1) ^ 20'h00A6;
        end
    endfunction

    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire [19:0] q;
    wire [8:0] raddr;
    reg armed = 1'b0;
    reg we = 1'b0;
    reg [8:0] waddr = 9'd0;
    reg [19:0] wdata = 20'd0;
    reg [8:0] addr_d = 9'd0;
    reg [8:0] addr_d2 = 9'd0;
    reg [19:0] early = 20'd0;
    reg [19:0] late = 20'd0;
    reg [15:0] sampled = 16'd0;

    assign raddr = gp_out[8:0];

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        waddr <= raddr;
        wdata <= gp_out[28:9];
        we <= armed && gp_out[31];
        addr_d <= raddr;
        addr_d2 <= addr_d;
        if (addr_d != addr_d2)
            early <= q;
        else if (raddr == addr_d)
            late <= q;
        sampled <= gp_out[29] ? late[15:0] : early[15:0];
    end

    (* keep *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1),
        .INIT(packed_init())
    ) mem (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(waddr),
        .A1DATA(wdata),
        .A1EN(we),
        .A1BE(2'b11),
        .B1ADDR(raddr),
        .B1DATA(q),
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
