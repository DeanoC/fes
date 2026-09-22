// Eight BEL-locked 1024x10 lanes. GPI signature 0xD893.
// Address [12:10] selects the block and [9:0] selects the lane.
module top #(
    parameter [15:0] SIGNATURE = 16'hD893
) (
    input wire FPGA_CLK1_50
);
    function automatic [10239:0] packed_init;
        input integer bank;
        integer address;
        integer word;
        begin
            packed_init = 10240'b0;
            for (address = 0; address < 1024; address = address + 1) begin
                if (address == 0)
                    word = 32'h100 + bank;
                else
                    word = (address * 32'd73) ^ (address >> 1) ^ 32'h000000A6;
                packed_init[address * 10 +: 10] = word[9:0];
            end
        end
    endfunction

    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire [12:0] full_addr;
    wire [9:0] lane_addr;
    wire [2:0] bank;
    wire [9:0] q0;
    wire [9:0] q1;
    wire [9:0] q2;
    wire [9:0] q3;
    wire [9:0] q4;
    wire [9:0] q5;
    wire [9:0] q6;
    wire [9:0] q7;
    reg [9:0] q;
    reg [15:0] sampled = 16'd0;
    (* keep *) reg [1:0] ring = 2'b01;

    assign full_addr = gp_out[12:0];
    assign lane_addr = full_addr[9:0];
    assign bank = full_addr[12:10];

    always @* begin
        case (bank)
            3'd0: q = q0;
            3'd1: q = q1;
            3'd2: q = q2;
            3'd3: q = q3;
            3'd4: q = q4;
            3'd5: q = q5;
            3'd6: q = q6;
            default: q = q7;
        endcase
    end

    always @(posedge FPGA_CLK1_50) begin
        sampled <= {6'd0, q};
        ring <= {ring[0], ring[1]};
    end

    (* keep, BEL = "MISTRAL_M10K.26.1.0", FES_SLOT = 1 *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init(0))
    ) lane0 (
        .CLK1(1'b0),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(lane_addr),
        .B1DATA(q0),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    (* keep, BEL = "MISTRAL_M10K.26.2.0", FES_SLOT = 1 *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init(1))
    ) lane1 (
        .CLK1(1'b0),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(lane_addr),
        .B1DATA(q1),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    (* keep, BEL = "MISTRAL_M10K.26.5.0", FES_SLOT = 1 *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init(2))
    ) lane2 (
        .CLK1(1'b0),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(lane_addr),
        .B1DATA(q2),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    (* keep, BEL = "MISTRAL_M10K.26.6.0", FES_SLOT = 1 *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init(3))
    ) lane3 (
        .CLK1(1'b0),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(lane_addr),
        .B1DATA(q3),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    (* keep, BEL = "MISTRAL_M10K.26.9.0", FES_SLOT = 1 *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init(4))
    ) lane4 (
        .CLK1(1'b0),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(lane_addr),
        .B1DATA(q4),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    (* keep, BEL = "MISTRAL_M10K.26.10.0", FES_SLOT = 1 *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init(5))
    ) lane5 (
        .CLK1(1'b0),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(lane_addr),
        .B1DATA(q5),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    (* keep, BEL = "MISTRAL_M10K.26.13.0", FES_SLOT = 1 *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init(6))
    ) lane6 (
        .CLK1(1'b0),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(lane_addr),
        .B1DATA(q6),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    (* keep, BEL = "MISTRAL_M10K.26.14.0", FES_SLOT = 1 *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init(7))
    ) lane7 (
        .CLK1(1'b0),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(lane_addr),
        .B1DATA(q7),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, sampled};
endmodule
