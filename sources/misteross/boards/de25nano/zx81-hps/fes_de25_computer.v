// SPDX-License-Identifier: GPL-2.0-or-later
// DE25 diagnostic transport: atomic Avalon GPO writes, coherent GPI reads.
// Transport state survives execution reset. Any transport fault requires FPGA
// reconfiguration; no local reset is allowed to manufacture a new GP epoch.
`include "generated/fes_de25_computer.vh"
`include "fes_computer.vh"
`include "zx81_bus_pack.vh"
`ifndef FES_DE25_COMPUTER_BUILD_ID
`define FES_DE25_COMPUTER_BUILD_ID 128'h00000000000000000000000000000000
`endif

module fes_de25_computer #(
    parameter [127:0] BUILD_ID = `FES_DE25_COMPUTER_BUILD_ID,
    parameter EXTERNAL_RAM = 0
) (
    input wire clk,
    input wire reset_n,
    input wire pixel_clk,
    input wire [`FES_DE25_COMPUTER_ADDRESS_BITS-1:0] address,
    input wire read,
    input wire write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata = 32'd0,
    output reg readdatavalid = 1'b0,
    output wire waitrequest,
    output reg writeresponsevalid = 1'b0,
    output reg [1:0] response = 2'b00,
    output wire [23:0] video_rgb,
    output wire video_de,
    output wire video_hs,
    output wire video_vs,
    input wire expansion_running,
    input wire [7:0] diagnostic_address,
    input wire [19:0] expansion_response,
    output wire [45:0] expansion_request,
    output wire [7:0] memory_peek
);
    reg first_seen = 1'b0;
    reg healthy = 1'b0;
    reg [31:0] fault_flags = 32'd0;
    reg [31:0] gpo = 32'd0;
    wire [31:0] gpi;
    wire reset_fault = first_seen && !healthy;
    wire [31:0] faults = fault_flags |
        (reset_fault ? `FES_DE25_COMPUTER_FAULT_RESET : 32'd0);
    wire fault_active = faults != 32'd0;
    wire transport_ready = reset_n && first_seen && healthy && !fault_active;
    wire gp_busy = ((gpo & `FES_COMPUTER_REQUEST_MASK) != 32'd0) !=
                   ((gpi & `FES_COMPUTER_ACK_MASK) != 32'd0);
    wire [31:0] status =
        (transport_ready ? `FES_DE25_COMPUTER_STATUS_READY : 32'd0) |
        (gp_busy ? `FES_DE25_COMPUTER_STATUS_BUSY : 32'd0) |
        (fault_active ? `FES_DE25_COMPUTER_STATUS_FAULT : 32'd0);
    wire [31:0] byte_address = {{(32-`FES_DE25_COMPUTER_ADDRESS_BITS){1'b0}}, address};
    reg [31:0] read_value;
    reg read_address_valid;
    reg [31:0] write_fault;
    wire [31:0] read_fault = read && !read_address_valid ?
        `FES_DE25_COMPUTER_FAULT_ADDRESS : 32'd0;
    wire access_error = (read && !read_address_valid) ||
                        (write && (fault_active || write_fault != 32'd0));

    wire exec_reset;
    wire [143:0] keyboard_rows;
    wire [39:0] keyboard;
    wire machine_reset = !transport_ready || exec_reset || (EXTERNAL_RAM && !expansion_running);
    wire [13:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable;
    wire [1:0] safe_media_write_enable = transport_ready ? media_write_enable : 2'b00;
    wire [1:0] unit0_state;
    wire [31:0] unit0_size;
    wire tape_ready_stored;
    wire tape_ready = transport_ready && tape_ready_stored;
    wire [14:0] tape_size;
    wire [7:0] tape_data;
    wire [13:0] tape_addr;
    wire tape_busy;
    wire ce_6m5, video_pixel, hblank, vblank;
    wire [15:0] cpu_addr;
    wire halt_n;
    wire [7:0] red, green, blue;
    wire de, hs, vs;
`ifdef QUARTUS
    localparam FIRMWARE_INIT = "zx8x.mif";
`else
    localparam FIRMWARE_INIT = "zx8x.hex";
`endif

    wire [15:0] bus_address;
    wire [7:0] bus_data;
    wire bus_mreq_n, bus_iorq_n, bus_rd_n, bus_wr_n, bus_m1_n, bus_rfsh_n, bus_clock;
    wire [15:0] peek_address = EXTERNAL_RAM ? {8'h40, diagnostic_address} : 16'd0;
    wire [19:0] machine_response;
    generate if (EXTERNAL_RAM) begin : external_bus
        (* preserve, dont_merge, noprune *) reg [45:0] bus_request = 0;
        (* preserve, dont_merge *) reg [19:0] response_q = 0;
        always @(posedge clk) begin
            response_q <= expansion_response;
            // Fence the whole CPU bus independently of enable-scheduled CPU
            // reset. The existing PR drain/settle contract then retires it.
            bus_request <= {!machine_reset, bus_clock, peek_address[13:0],
                !machine_reset ? {bus_rfsh_n, bus_m1_n, bus_wr_n, bus_rd_n,
                    bus_iorq_n, bus_mreq_n, bus_data, bus_address} : {6'b111111, 24'd0}};
        end
        assign expansion_request = bus_request;
        assign machine_response = response_q;
    end else begin : internal_bus
        assign expansion_request = 46'd0;
        assign machine_response = 20'd0;
    end endgenerate

    // These two registers deliberately have different reset behavior. The
    // first qualified release arms the epoch. A later reset clears healthy
    // asynchronously, even with clk stopped, and it can never become healthy
    // again. The GP word, mailbox state and sticky causes are never reset alone.
    always @(posedge clk)
        if (reset_n) first_seen <= 1'b1;
    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) healthy <= 1'b0;
        else if (!first_seen) healthy <= 1'b1;
    end

    assign waitrequest = !reset_n || !first_seen;
    always @* begin
        read_address_valid = 1'b1;
        case (byte_address)
            `FES_DE25_COMPUTER_ID_OFFSET: read_value = `FES_DE25_COMPUTER_IDENTITY;
            `FES_DE25_COMPUTER_VERSION_OFFSET: read_value = `FES_DE25_COMPUTER_VERSION;
            `FES_DE25_COMPUTER_STATUS_OFFSET: read_value = status;
            `FES_DE25_COMPUTER_GPO_OFFSET: read_value = gpo;
            `FES_DE25_COMPUTER_GPI_OFFSET: read_value = gpi;
            `FES_DE25_COMPUTER_FAULT_OFFSET: read_value = faults;
            default: begin read_value = 32'd0; read_address_valid = 1'b0; end
        endcase
        write_fault = 32'd0;
        if (write && !fault_active) begin
            if (byte_address != `FES_DE25_COMPUTER_GPO_OFFSET)
                write_fault = `FES_DE25_COMPUTER_FAULT_ADDRESS;
            else if (byteenable != 4'hf)
                write_fault = `FES_DE25_COMPUTER_FAULT_PARTIAL;
            else if (gp_busy)
                write_fault = `FES_DE25_COMPUTER_FAULT_BUSY;
            else if ((writedata & `FES_COMPUTER_REQUEST_MASK) == (gpo & `FES_COMPUTER_REQUEST_MASK))
                write_fault = `FES_DE25_COMPUTER_FAULT_TOGGLE;
        end
    end

    // The bus and mailbox use one clock. Reads capture one complete pre-edge
    // word; write completion acknowledges GPO storage, not GP command execution.
    always @(posedge clk) begin
        if (!waitrequest) begin
            fault_flags <= fault_flags | write_fault | read_fault;
            if (write && !fault_active && write_fault == 32'd0)
                gpo <= writedata;
        end
    end
    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) begin
            readdata <= 32'd0;
            readdatavalid <= 1'b0;
            writeresponsevalid <= 1'b0;
            response <= 2'b00;
        end else begin
            readdatavalid <= read && !waitrequest;
            writeresponsevalid <= write && !waitrequest;
            response <= access_error ? 2'b10 : 2'b00;
            if (read && !waitrequest) readdata <= read_value;
        end
    end

    fes_computer_mailbox #(
        .ENABLE_KEYBOARD(1), .ENABLE_ZX81_TAPE(1), .MEDIA_AW(14),
        .UNIT0_MIN(`FES_COMPUTER_ZX81_TAPE_MIN_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_ZX81_TAPE_MAX_BYTES)
    ) gp_mailbox (
        .clk(clk), .gpo(gpo), .build_id(BUILD_ID), .gpi(gpi),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows), .controller_buttons(),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_ready(1'b1),
        .media_write_busy(tape_busy), .media_changed(1'b0), .media_frozen(),
        .media_read_req(), .media_read_addr(), .media_read_ready(1'b0), .media_read_data(8'd0),
        .unit0_state(unit0_state), .unit0_size(unit0_size)
    );
    zx81_keyboard #(.STABLE_CYCLES(100000)) keyboard_map (
        .clk(clk), .reset(machine_reset), .rows(keyboard_rows), .matrix(keyboard));
    zx81_tape_store tape (
        .clk(clk), .unit_state(unit0_state), .unit_size(unit0_size),
        .write_addr(media_write_addr), .write_data(media_write_data),
        .write_enable(safe_media_write_enable), .tape_addr(tape_addr),
        .tape_ready(tape_ready_stored), .tape_size(tape_size), .tape_data(tape_data));
    zx81_machine #(.EXTERNAL_RAM(EXTERNAL_RAM), .FIRMWARE_INIT(FIRMWARE_INIT),
        .CLOCK_NUMERATOR(13), .CLOCK_DENOMINATOR(200)) machine (
        .clk_sys(clk), .reset(machine_reset), .keyboard(keyboard),
        .tape_ready(tape_ready), .tape_size(tape_size), .tape_data(tape_data),
        .tape_addr_out(tape_addr), .tape_busy(tape_busy),
        .save_ready(), .save_busy(), .save_changed(), .save_size(), .save_addr(14'd0), .save_q(),
        .ce_6m5(ce_6m5), .video_pixel(video_pixel), .hblank(hblank), .vblank(vblank),
        .hsync_out(), .vsync_out(), .halt_n(halt_n), .cpu_addr(cpu_addr),
        .peek_addr(peek_address), .peek_data(memory_peek), .ram_address(), .ram_write_data(), .ram_write_enable(),
        .external_ram_data(8'hff), .external_peek_data(8'hff),
        .bus_addr(bus_address), .bus_wdata(bus_data), .bus_mreq_n(bus_mreq_n), .bus_iorq_n(bus_iorq_n),
        .bus_rd_n(bus_rd_n), .bus_wr_n(bus_wr_n), .bus_m1_n(bus_m1_n), .bus_rfsh_n(bus_rfsh_n),
        .bus_cpu_clock(bus_clock), .bus_rdata(machine_response[`ZX81_BUS_DRD]),
        .bus_peek_data(machine_response[`ZX81_BUS_PEEK_D]), .bus_dsel(machine_response[`ZX81_BUS_DSEL]),
        .bus_romcs(machine_response[`ZX81_BUS_ROMCS]), .bus_wait(machine_response[`ZX81_BUS_WAIT]),
        .bus_ram_present(machine_response[`ZX81_BUS_RAM_PRESENT]));
    fes_de25_zx81_video video (
        .reset_n(transport_ready), .held(machine_reset),
        .clk_sys(clk), .ce_6m5(ce_6m5), .zx_pixel(video_pixel), .hblank(hblank), .vblank(vblank),
        .pixel_clk(pixel_clk), .red(red), .green(green), .blue(blue), .de(de), .hsync(hs), .vsync(vs));
    assign video_rgb = transport_ready ? {red, green, blue} : 24'd0;
    assign video_de = transport_ready && de;
    assign video_hs = transport_ready && hs;
    assign video_vs = transport_ready && vs;
endmodule
