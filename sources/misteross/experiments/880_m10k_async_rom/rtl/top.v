// Native 1024x10 read-only async M10K. The write clock is folded; nextpnr
// borrows FPGA_CLK1_50 for the physical flow-through read path.
module top #(
    parameter [15:0] SIGNATURE = 16'hD880
) (
    input wire FPGA_CLK1_50
);
    function automatic [10239:0] packed_init;
        integer address;
        integer word;
        begin
            packed_init = 10240'b0;
            for (address = 0; address < 1024; address = address + 1) begin
                word = (address * 32'd73) ^ (address >> 1) ^ 32'h000000A6;
                packed_init[address * 10 +: 10] = word[9:0];
            end
        end
    endfunction

    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire [9:0] q;
    wire [9:0] addr;
    reg [15:0] sampled = 16'd0;
    (* keep *) reg [1:0] ring = 2'b01;

    assign addr = gp_out[9:0];

    always @(posedge FPGA_CLK1_50) begin
        sampled <= {6'd0, q};
        ring <= {ring[0], ring[1]};
    end

    (* keep *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init())
    ) mem (
        .CLK1(1'b0),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(addr),
        .B1DATA(q),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, sampled};
endmodule
