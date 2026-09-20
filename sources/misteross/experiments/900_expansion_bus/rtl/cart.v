// Independent cart A: one BEL-locked slot cell and the closed INIT oracle.
// Speaks the empty-socket plug names. Signature stays in the 901 shell.
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

    wire [9:0] q;
    assign plug_rdata = q;

    (* keep, BEL = "MISTRAL_M10K.26.1.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(10),
        .CFG_DBITS(10),
        .INIT(packed_init())
    ) slot_cell (
        .CLK1(FPGA_CLK1_50),
        .A1ADDR(10'd0),
        .A1DATA(10'd0),
        .A1EN(1'b1),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
endmodule
