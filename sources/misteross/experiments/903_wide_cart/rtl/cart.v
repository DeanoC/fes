// Independent cart B: four slot cells and a tiny address decode into the
// same reserved rect as cart A. Plug names match the 901 shell.
module cart (
    input wire FPGA_CLK1_50,
    input wire [15:0] plug_addr,
    output wire [9:0] plug_rdata
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

    wire [9:0] q0, q1, q2, q3;
    wire [1:0] sel;
    assign sel = plug_addr[11:10];
    assign plug_rdata = (sel == 2'd0) ? q0 :
                        (sel == 2'd1) ? (q1 ^ 10'd17) :
                        (sel == 2'd2) ? (q2 ^ 10'd34) : (q3 ^ 10'd51);

    (* keep, BEL = "MISTRAL_M10K.26.1.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init())
    ) slot_cell0 (
        .CLK1(FPGA_CLK1_50),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q0),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.2.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init())
    ) slot_cell1 (
        .CLK1(FPGA_CLK1_50),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q1),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.5.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init())
    ) slot_cell2 (
        .CLK1(FPGA_CLK1_50),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q2),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.6.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init())
    ) slot_cell3 (
        .CLK1(FPGA_CLK1_50),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q3),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
endmodule
