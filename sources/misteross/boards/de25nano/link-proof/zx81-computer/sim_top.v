// SPDX-License-Identifier: GPL-2.0-or-later
// Both real endpoints and the actual RAM personas. Selection alone is modeled.
module fes_de25_combined_sim (
    input wire clk, reset_n, pixel_clk, persona_b, drop_response,
    input wire [4:0] address,
    input wire read, write, input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output wire [31:0] readdata,
    output wire readdatavalid, writeresponsevalid, waitrequest,
    output wire [1:0] response,
    output wire [23:0] video_rgb,
    output wire video_de, video_hs, video_vs,
    input wire [5:0] pr_address,
    input wire pr_read, pr_write, input wire [31:0] pr_writedata,
    output wire [31:0] pr_readdata,
    output wire pr_readdatavalid, pr_writeresponsevalid, pr_waitrequest,
    output wire [1:0] pr_response,
    output wire frozen, running, slot_reset,
    output wire [31:0] pr_retired,
    output wire [45:0] bus_request
);
    wire req, a_valid, b_valid;
    wire [7:0] diagnostic_address, memory_peek;
    wire [31:0] a_data, b_data;
    wire [19:0] a_bus, b_bus;
    fes_de25_computer #(.EXTERNAL_RAM(1)) computer (
        .clk(clk), .reset_n(reset_n), .pixel_clk(pixel_clk),
        .video_ready(1'b1), // external HDMI completion modeled in this protocol proof
        .address(address), .read(read), .write(write), .byteenable(byteenable), .writedata(writedata),
        .readdata(readdata), .readdatavalid(readdatavalid), .writeresponsevalid(writeresponsevalid),
        .waitrequest(waitrequest), .response(response), .video_rgb(video_rgb),
        .video_de(video_de), .video_hs(video_hs), .video_vs(video_vs),
        .expansion_running(running), .diagnostic_address(diagnostic_address),
        .expansion_request(bus_request), .expansion_response(persona_b ? b_bus : a_bus), .memory_peek(memory_peek));
    fes_link_shell shell (
        .clk(clk), .reset_n(reset_n), .address(pr_address), .read(pr_read), .write(pr_write),
        .byteenable(4'hf), .writedata(pr_writedata), .memory_peek(memory_peek),
        .readdata(pr_readdata), .readdatavalid(pr_readdatavalid), .writeresponsevalid(pr_writeresponsevalid),
        .response(pr_response), .waitrequest(pr_waitrequest), .req(req), .slot_address(diagnostic_address),
        .slot_reset(slot_reset), .rsp_valid(!drop_response && (persona_b ? b_valid : a_valid)),
        .rsp_data(persona_b ? b_data : a_data), .frozen(frozen), .running(running),
        .isolated_data(), .static_count(), .retired(pr_retired), .last_data());
    fes_link_persona_a a (.clk(clk), .reset(slot_reset || persona_b), .req(req), .address(diagnostic_address),
        .bus_request(bus_request), .bus_response(a_bus), .rsp_valid(a_valid), .rsp_data(a_data));
    fes_link_persona_b b (.clk(clk), .reset(slot_reset || !persona_b), .req(req), .address(diagnostic_address),
        .bus_request(bus_request), .bus_response(b_bus), .rsp_valid(b_valid), .rsp_data(b_data));
endmodule
