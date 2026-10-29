// Logical transition model only: both real personas, static shell, explicit mux.
// This does not model SDM/configuration RAM, clock glitches or physical PR.
module fes_link_sim (
    input wire clk, reset_n, freeze_req, persona_b,
    output wire req, slot_reset, freeze_ack, rsp_valid,
    output wire [7:0] address,
    output wire [31:0] rsp_data, isolated_data, static_count, retired, last_data
);
    wire a_valid, b_valid;
    wire [31:0] a_data, b_data;
    fes_link_shell shell_inst (.*);
    fes_link_persona_a a (.clk(clk), .reset(slot_reset), .req(req),
        .address(address), .rsp_valid(a_valid), .rsp_data(a_data));
    fes_link_persona_b b (.clk(clk), .reset(slot_reset), .req(req),
        .address(address), .rsp_valid(b_valid), .rsp_data(b_data));
    assign rsp_valid = persona_b ? b_valid : a_valid;
    assign rsp_data = persona_b ? b_data : a_data;
endmodule
