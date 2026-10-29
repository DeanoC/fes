// Same registered socket, two independent PR implementations.
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
    reg [31:0] rom_q;
    reg rom_valid;
    reg [31:0] counter;
    initial $readmemh(ROM_FILE, rom);
    always @(posedge clk) begin
        if (req) rom_q <= rom[address];
        if (reset) begin
            counter <= 0;
            rom_valid <= 0;
            rsp_valid <= 0;
            rsp_data <= 0;
        end else begin
            rom_valid <= req;
            rsp_valid <= rom_valid;
            if (req) counter <= counter + STEP;
            if (rom_valid) rsp_data <= rom_q ^ counter ^ ID;
        end
    end
endmodule

module fes_link_persona_a (
    input wire clk, reset, req,
    input wire [7:0] address,
    output wire rsp_valid,
    output wire [31:0] rsp_data
);
    fes_link_persona_core #(.ID(32'hc001a001), .STEP(1),
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
    fes_link_persona_core #(.ID(32'hc002b002), .STEP(3),
        .ROM_FILE("persona_b.hex")) core (
        .clk(clk), .reset(reset), .req(req), .address(address),
        .rsp_valid(rsp_valid), .rsp_data(rsp_data));
endmodule
