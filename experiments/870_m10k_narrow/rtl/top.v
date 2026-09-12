// Equal-width 8192x1 true-dual-port M10K. Locked Yosys has no narrow TDP
// inference, so the primitive is instantiated directly.
module top #(
    parameter [15:0] SIGNATURE = 16'hD870
) (
    input wire FPGA_CLK1_50
);
    function automatic [8191:0] packed_init;
        integer address;
        integer word;
        begin
            packed_init = 8192'b0;
            for (address = 0; address < 8192; address = address + 1) begin
                word = (address * 32'd73) ^ (address >> 1) ^ 32'h000000A6;
                packed_init[address] = word[0];
            end
        end
    endfunction

    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire q;
    wire unused_b;
    wire [12:0] addr;
    reg armed = 1'b0;
    reg we = 1'b0;
    reg [12:0] waddr = 13'd0;
    reg wdata = 1'b0;
    reg [15:0] sampled = 16'd0;

    assign addr = gp_out[12:0];

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        waddr <= addr;
        wdata <= gp_out[13];
        we <= armed && gp_out[31];
        sampled <= {15'd0, q};
    end

    (* keep *)
    MISTRAL_M10K_TDP #(
        .CFG_ABITS(13),
        .CFG_DBITS(1),
        .INIT(packed_init())
    ) mem (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(waddr),
        .A1DATA(wdata),
        .A1EN(gp_out[30]),
        .A1WE(we),
        .A1Q(q),
        .B1ADDR(13'd0),
        .B1DATA(1'b0),
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
