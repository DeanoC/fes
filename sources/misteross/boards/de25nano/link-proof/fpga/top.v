module fes_link_proof (
    input wire CLOCK0_50,
    // Virtual diagnostic input: no physical/HPS control path is implemented.
    input wire freeze_req,
    output wire [7:0] LEDR
);
    wire ninit_done;
    reset_release release_inst (.ninit_done(ninit_done));
    (* preserve, dont_merge *) reg [1:0] reset_sync;
    always @(posedge CLOCK0_50 or posedge ninit_done) begin
        if (ninit_done) reset_sync <= 0;
        else reset_sync <= {reset_sync[0], 1'b1};
    end
    wire req, slot_reset, rsp_valid, freeze_ack;
    wire [7:0] address;
    wire [31:0] rsp_data, isolated_data, static_count, retired, last_data;
    fes_link_shell shell_inst (
        .clk(CLOCK0_50), .reset_n(reset_sync[1]), .freeze_req(freeze_req),
        .req(req), .address(address), .slot_reset(slot_reset),
        .rsp_valid(rsp_valid), .rsp_data(rsp_data), .freeze_ack(freeze_ack),
        .isolated_data(isolated_data), .static_count(static_count),
        .retired(retired), .last_data(last_data)
    );
    fes_link_persona_a slot_inst (
        .clk(CLOCK0_50), .reset(slot_reset), .req(req), .address(address),
        .rsp_valid(rsp_valid), .rsp_data(rsp_data)
    );
    assign LEDR = {freeze_ack, ^last_data, retired[15:13], static_count[25:23]};
endmodule
