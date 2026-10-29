// Same registered socket, two independent PR implementations.
`include "generated/fes_de25_pr_control.vh"
module fes_link_persona_core #(
    parameter [31:0] ID = 32'hc001a001,
    parameter integer STEP = 1,
    parameter ROM_FILE = "persona_a.hex"
) (
    input wire clk,
    input wire reset,
    input wire req,
    input wire [7:0] address,
    output reg rsp_valid,
    output reg [31:0] rsp_data
);
    (* ramstyle = "M20K" *) reg [31:0] rom [0:255];
    // Dedicated registers terminate every reconfigurable control/data input.
    (* preserve, dont_merge *) reg request_q = 1'b0;
    (* preserve, dont_merge *) reg reset_q = 1'b1;
    (* preserve, dont_merge *) reg [7:0] address_q = 8'd0;
    reg [31:0] rom_q;
    reg rom_valid;
    reg id_read;
    reg [31:0] counter;
    initial $readmemh(ROM_FILE, rom);
    always @(posedge clk) begin
        request_q <= req;
        reset_q <= reset;
        address_q <= address;
        if (request_q) rom_q <= rom[address_q];
        if (reset_q) begin
            counter <= 0;
            rom_valid <= 0;
            id_read <= 0;
            rsp_valid <= 0;
            rsp_data <= 0;
        end else begin
            rom_valid <= request_q;
            id_read <= address_q == 8'd0;
            rsp_valid <= rom_valid;
            if (request_q) counter <= counter + STEP;
            if (rom_valid) rsp_data <= id_read ? ID : (rom_q ^ counter ^ ID);
        end
    end
endmodule

module fes_link_persona_a (
    input wire clk, reset, req,
    input wire [7:0] address,
    output wire rsp_valid,
    output wire [31:0] rsp_data
);
    fes_link_persona_core #(.ID(`FES_DE25_PR_CONTROL_PERSONA_A), .STEP(1),
        .ROM_FILE("persona_a.hex")) core (
        .clk(clk), .reset(reset), .req(req), .address(address),
        .rsp_valid(rsp_valid), .rsp_data(rsp_data));
endmodule

module fes_link_persona_b (
    input wire clk, reset, req,
    input wire [7:0] address,
    output wire rsp_valid,
    output wire [31:0] rsp_data
);
    fes_link_persona_core #(.ID(`FES_DE25_PR_CONTROL_PERSONA_B), .STEP(3),
        .ROM_FILE("persona_b.hex")) core (
        .clk(clk), .reset(reset), .req(req), .address(address),
        .rsp_valid(rsp_valid), .rsp_data(rsp_data));
endmodule
