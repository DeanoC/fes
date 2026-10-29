// Actual HPS register endpoint and personas; explicit mux models selection only.
// SDM programming, HPS bridges and M20K primitives are not simulated here.
module fes_link_sim #(
    parameter integer TIMEOUT_CYCLES = 64,
    parameter [127:0] BUILD_ID = 128'h112233445566778899aabbccddeeff00
) (
    input wire clk, reset_n, persona_b,
    input wire [5:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    input wire drop_response, inject_response,
    output wire [31:0] readdata,
    output wire readdatavalid, writeresponsevalid, waitrequest,
    output wire [1:0] response,
    output wire req, slot_reset, frozen, running,
    output wire [7:0] slot_address,
    output wire [31:0] isolated_data, static_count, retired, last_data
);
    wire a_valid, b_valid;
    wire [31:0] a_data, b_data;
    wire selected_valid = persona_b ? b_valid : a_valid;
    wire [31:0] selected_data = persona_b ? b_data : a_data;
    wire rsp_valid = inject_response || (selected_valid && !drop_response);
    wire [31:0] rsp_data = inject_response ? 32'hbad0bad0 : selected_data;
    fes_link_shell #(.BUILD_ID(BUILD_ID), .TIMEOUT_CYCLES(TIMEOUT_CYCLES)) shell_inst (
        .clk(clk), .reset_n(reset_n), .address(address), .read(read), .write(write),
        .byteenable(byteenable), .writedata(writedata), .readdata(readdata),
        .readdatavalid(readdatavalid), .writeresponsevalid(writeresponsevalid),
        .waitrequest(waitrequest), .response(response), .req(req), .slot_address(slot_address),
        .slot_reset(slot_reset), .rsp_valid(rsp_valid), .rsp_data(rsp_data),
        .frozen(frozen), .running(running), .isolated_data(isolated_data),
        .static_count(static_count), .retired(retired), .last_data(last_data));
    fes_link_persona_a a (.clk(clk), .reset(slot_reset), .req(req),
        .address(slot_address), .rsp_valid(a_valid), .rsp_data(a_data));
    fes_link_persona_b b (.clk(clk), .reset(slot_reset), .req(req),
        .address(slot_address), .rsp_valid(b_valid), .rsp_data(b_data));
endmodule
