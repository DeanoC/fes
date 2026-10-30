module fes_zx81_pr_sim (
    input wire clk, reset_n, persona_b,
    input wire [5:0] address,
    input wire read, write,
    input wire [31:0] writedata,
    input wire debug_bus,
    input wire [45:0] debug_request,
    output wire [19:0] debug_response,
    output wire [31:0] readdata, last_data, retired,
    output wire readdatavalid, writeresponsevalid, waitrequest,
    output wire [1:0] response,
    output wire frozen, running, slot_reset, diagnostic_req,
    output wire diagnostic_rsp_valid,
    output wire [31:0] diagnostic_rsp_data,
    output wire [7:0] peek_data, diagnostic_address,
    output wire [15:0] cpu_addr,
    output wire [45:0] machine_request,
    output wire memory_write,
    output wire [13:0] memory_address,
    output wire [7:0] memory_data,
    output wire [19:0] static_response
);
    wire a_valid, b_valid;
    wire [31:0] a_data, b_data;
    wire [19:0] a_bus, b_bus;
    wire [45:0] guarded_debug = {debug_request[45] && running, debug_request[44:30],
        running ? debug_request[29:0] : {6'b111111, 24'd0}};
    // The test generator includes the same first request FF as the real
    // static machine adapter, so freeze probes include the full bus pipeline.
    reg [45:0] debug_request_q = 0;
    always @(posedge clk) debug_request_q <= guarded_debug;
    wire [45:0] bus_request = debug_bus ? debug_request_q : machine_request;
    wire [19:0] bus_response = persona_b ? b_bus : a_bus;
    assign debug_response = bus_response;
    assign diagnostic_rsp_valid = persona_b ? b_valid : a_valid;
    assign diagnostic_rsp_data = persona_b ? b_data : a_data;
    assign memory_write = b.core.write_enable && persona_b;
    assign memory_address = b.core.bus_q[13:0];
    assign memory_data = b.core.bus_q[23:16];
    assign static_response = machine.response_q;
    fes_link_shell shell (
        .clk(clk), .reset_n(reset_n), .address(address), .read(read), .write(write),
        .byteenable(4'hf), .writedata(writedata), .memory_peek(peek_data),
        .readdata(readdata), .readdatavalid(readdatavalid), .writeresponsevalid(writeresponsevalid),
        .response(response), .waitrequest(waitrequest), .req(diagnostic_req), .slot_address(diagnostic_address),
        .slot_reset(slot_reset), .rsp_valid(persona_b ? b_valid : a_valid),
        .rsp_data(persona_b ? b_data : a_data), .frozen(frozen), .running(running),
        .isolated_data(), .static_count(), .retired(retired), .last_data(last_data));
    fes_de25_zx81_static machine (.clk(clk), .running(running), .diagnostic_address(diagnostic_address),
        .bus_request(machine_request), .bus_response(bus_response), .peek_data(peek_data), .cpu_addr(cpu_addr));
    fes_link_persona_a a (.clk(clk), .reset(slot_reset), .req(diagnostic_req), .address(diagnostic_address),
        .bus_request(bus_request), .bus_response(a_bus), .rsp_valid(a_valid), .rsp_data(a_data));
    fes_link_persona_b b (.clk(clk), .reset(slot_reset), .req(diagnostic_req), .address(diagnostic_address),
        .bus_request(bus_request), .bus_response(b_bus), .rsp_valid(b_valid), .rsp_data(b_data));
endmodule
