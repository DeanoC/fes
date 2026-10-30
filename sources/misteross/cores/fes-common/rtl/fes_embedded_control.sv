// SPDX-License-Identifier: GPL-3.0-or-later
// Portable control/drain endpoint. Board glue must supply the CSR aperture,
// reset/clock contract and a single-ID AXI read port. No Nano pinout is implied.
`include "fes_embedded_control.vh"
module fes_embedded_control #(
    parameter [127:0] BUILD_ID = 128'h1,
    parameter [31:0] ABI_ID = 32'h1,
    parameter integer DATA_WIDTH = 128
)(
    input wire clk, reset_n,
    input wire csr_valid, csr_write,
    input wire [7:0] csr_address,
    input wire [31:0] csr_wdata,
    input wire [3:0] csr_wstrb,
    output wire csr_ready,
    output reg csr_response_valid, csr_error,
    output reg [31:0] csr_rdata,
    input wire csr_response_ready,
    input wire reader_valid,
    input wire [31:0] reader_address,
    input wire [7:0] reader_length,
    output wire reader_ready,
    output wire reader_rvalid,
    output wire [DATA_WIDTH-1:0] reader_rdata,
    output wire reader_rlast,
    input wire reader_rready,
    output wire reader_abort,
    output wire m_arvalid,
    output wire [31:0] m_araddr,
    output wire [7:0] m_arlen,
    output wire [2:0] m_arsize,
    output wire [1:0] m_arburst,
    output wire [3:0] m_arid,
    input wire m_arready,
    input wire m_rvalid,
    input wire [DATA_WIDTH-1:0] m_rdata,
    input wire m_rlast,
    input wire [1:0] m_rresp,
    input wire [3:0] m_rid,
    output wire m_rready
);
    reg enabled, draining, drained, fault;
    reg [63:0] generation, request_generation, request_cookie, last_cookie;
    reg [63:0] ack_generation, ack_cookie;
    reg skid_valid;
    reg [31:0] skid_address;
    reg [7:0] skid_length;
    reg [8:0] remaining [0:3];
    reg [1:0] head, tail;
    reg [2:0] count;
    localparam [2:0] AXI_SIZE = 3'($clog2(DATA_WIDTH/8));
    wire [31:0] csr_offset = {24'd0,csr_address};
    wire csr_fire = csr_valid && csr_ready;
    wire disable_now = csr_fire && csr_write &&
        csr_offset == `FES_EMBEDDED_REG_COMMAND &&
        csr_wdata == `FES_EMBEDDED_COMMAND_DISABLE && csr_wstrb == 4'hf;
    assign csr_ready = !csr_response_valid || csr_response_ready;
    assign reader_ready = enabled && !disable_now && !fault && !skid_valid && count < 4;
    assign reader_abort = !enabled || disable_now || fault;
    // An address already offered to AXI must remain valid/stable on disable.
    assign m_arvalid = skid_valid && count < 4;
    assign m_araddr = skid_address;
    assign m_arlen = skid_length;
    assign m_arsize = AXI_SIZE;
    assign m_arburst = 2'b01;
    assign m_arid = 0;
    wire ar_fire = m_arvalid && m_arready;
    wire bad_beat = m_rid != 0 || m_rresp != 0 ||
        (count != 0 && (m_rlast != (remaining[head] == 1)));
    assign m_rready = fault || (count != 0 &&
        (!enabled || disable_now || bad_beat || reader_rready));
    assign reader_rvalid = m_rvalid && count != 0 && enabled &&
        !disable_now && !fault && !bad_beat;
    assign reader_rdata = m_rdata;
    assign reader_rlast = m_rlast;
    wire r_fire = m_rvalid && m_rready;
    wire read_done = r_fire && count != 0 && remaining[head] == 1;
    wire bus_fault = m_rvalid && ((count == 0 && !ar_fire) || bad_beat);
    wire [31:0] status = {28'd0, fault, drained, draining, enabled};

    always @(posedge clk) begin
        if (!reset_n) begin
            enabled <= 0; draining <= 0; drained <= 0; fault <= 0;
            generation <= 0; request_generation <= 0; request_cookie <= 0;
            last_cookie <= 0; ack_generation <= 0; ack_cookie <= 0;
            skid_valid <= 0; skid_address <= 0; skid_length <= 0;
            head <= 0; tail <= 0; count <= 0;
            csr_response_valid <= 0; csr_error <= 0; csr_rdata <= 0;
            remaining[0] <= 0; remaining[1] <= 0;
            remaining[2] <= 0; remaining[3] <= 0;
        end else begin
            if (csr_response_ready) csr_response_valid <= 0;
            if (reader_valid && reader_ready) begin
                skid_valid <= 1;
                skid_address <= reader_address;
                skid_length <= reader_length;
            end
            if (ar_fire) begin
                skid_valid <= 0;
                remaining[tail] <= {1'b0,skid_length} + 9'd1;
                tail <= tail + 1'b1;
            end
            if (r_fire && count != 0) begin
                if (read_done) head <= head + 1'b1;
                else remaining[head] <= remaining[head] - 1'b1;
            end
            case ({ar_fire,read_done})
                2'b10: count <= count + 1'b1;
                2'b01: count <= count - 1'b1;
                default: begin end
            endcase
            // Never infer drain from reset, reader disable, a cycle budget or
            // RLAST alone. Every accepted beat and held address must finish.
            if (draining && !skid_valid && count == 0 && !m_rvalid && !fault) begin
                draining <= 0; drained <= 1;
                ack_generation <= generation; ack_cookie <= last_cookie;
            end
            if (csr_fire) begin
                csr_response_valid <= 1; csr_error <= 0; csr_rdata <= 0;
                if (csr_write) begin
                    if (csr_wstrb != 4'hf) csr_error <= 1;
                    else case (csr_offset)
                        `FES_EMBEDDED_REG_REQUEST_GENERATION_LO: request_generation[31:0] <= csr_wdata;
                        `FES_EMBEDDED_REG_REQUEST_GENERATION_HI: request_generation[63:32] <= csr_wdata;
                        `FES_EMBEDDED_REG_REQUEST_COOKIE_LO: request_cookie[31:0] <= csr_wdata;
                        `FES_EMBEDDED_REG_REQUEST_COOKIE_HI: request_cookie[63:32] <= csr_wdata;
                        `FES_EMBEDDED_REG_COMMAND: begin
                            if (csr_wdata == `FES_EMBEDDED_COMMAND_ENABLE && !enabled && !draining &&
                                !fault && !skid_valid && count == 0 && !m_rvalid && request_generation > generation) begin
                                generation <= request_generation; enabled <= 1; drained <= 0;
                                ack_generation <= 0; ack_cookie <= 0;
                            end else if (csr_wdata == `FES_EMBEDDED_COMMAND_DISABLE && enabled &&
                                !fault && request_generation == generation && generation != 0 &&
                                request_cookie > last_cookie) begin
                                enabled <= 0; draining <= 1; drained <= 0;
                                last_cookie <= request_cookie; ack_generation <= 0; ack_cookie <= 0;
                            end else csr_error <= 1;
                        end
                        default: csr_error <= 1;
                    endcase
                end else case (csr_offset)
                    `FES_EMBEDDED_REG_MAGIC: csr_rdata <= `FES_EMBEDDED_MAGIC;
                    `FES_EMBEDDED_REG_VERSION: csr_rdata <= `FES_EMBEDDED_VERSION;
                    `FES_EMBEDDED_REG_ABI: csr_rdata <= ABI_ID;
                    `FES_EMBEDDED_REG_BUILD0: csr_rdata <= BUILD_ID[31:0];
                    `FES_EMBEDDED_REG_BUILD1: csr_rdata <= BUILD_ID[63:32];
                    `FES_EMBEDDED_REG_BUILD2: csr_rdata <= BUILD_ID[95:64];
                    `FES_EMBEDDED_REG_BUILD3: csr_rdata <= BUILD_ID[127:96];
                    `FES_EMBEDDED_REG_GENERATION_LO: csr_rdata <= generation[31:0];
                    `FES_EMBEDDED_REG_GENERATION_HI: csr_rdata <= generation[63:32];
                    `FES_EMBEDDED_REG_REQUEST_GENERATION_LO: csr_rdata <= request_generation[31:0];
                    `FES_EMBEDDED_REG_REQUEST_GENERATION_HI: csr_rdata <= request_generation[63:32];
                    `FES_EMBEDDED_REG_REQUEST_COOKIE_LO: csr_rdata <= request_cookie[31:0];
                    `FES_EMBEDDED_REG_REQUEST_COOKIE_HI: csr_rdata <= request_cookie[63:32];
                    `FES_EMBEDDED_REG_STATUS: csr_rdata <= status;
                    `FES_EMBEDDED_REG_PENDING: csr_rdata <= {15'd0,skid_valid,13'd0,count};
                    `FES_EMBEDDED_REG_ACK_GENERATION_LO: csr_rdata <= ack_generation[31:0];
                    `FES_EMBEDDED_REG_ACK_GENERATION_HI: csr_rdata <= ack_generation[63:32];
                    `FES_EMBEDDED_REG_ACK_COOKIE_LO: csr_rdata <= ack_cookie[31:0];
                    `FES_EMBEDDED_REG_ACK_COOKIE_HI: csr_rdata <= ack_cookie[63:32];
                    default: csr_error <= 1;
                endcase
            end
            if (bus_fault) begin
                fault <= 1; enabled <= 0; draining <= 0; drained <= 0;
            end
        end
    end
endmodule
