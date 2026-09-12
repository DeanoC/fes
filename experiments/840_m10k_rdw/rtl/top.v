// True-dual-port 512x20 M10K with an explicit same-port write-through
// contract. Locked Yosys omits CFG_RDW_MODE_*, so OSS sets them after synth.
module top #(
    parameter [15:0] SIGNATURE = 16'hD840
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
    wire [19:0] unused_b;
    wire [8:0] addr;
    reg armed = 1'b0;
    reg we = 1'b0;
    reg [8:0] waddr = 9'd0;
    reg [19:0] wdata = 20'd0;
    reg [15:0] sampled = 16'd0;

    assign addr = gp_out[8:0];

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        waddr <= addr;
        wdata <= gp_out[28:9];
        we <= armed && gp_out[31];
        sampled <= q[15:0];
    end

    (* keep *)
    MISTRAL_M10K_TDP #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .INIT(packed_init())
    ) mem (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(waddr),
        .A1DATA(wdata),
        .A1EN(gp_out[30]),
        .A1WE(we),
        .A1Q(q),
        .B1ADDR(9'd0),
        .B1DATA(20'd0),
        .B1EN(1'b0),
        .B1WE(1'b0),
        .B1Q(unused_b),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, sampled};
endmodule
