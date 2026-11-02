// SPDX-License-Identifier: GPL-3.0-or-later
// Diagnostic Avalon aperture. The explicit DDR backend uses one bounded AXI
// burst at a time; the default on-chip responder never issues DDR traffic.
// Neither backend contains other FPGA masters or grants bridge ownership.
`ifndef FES_DE25_COMPUTER_BUILD_ID
`define FES_DE25_COMPUTER_BUILD_ID 128'h0
`endif

module fes_de25_embedded #(
    parameter [127:0] BUILD_ID = `FES_DE25_COMPUTER_BUILD_ID,
    parameter [31:0] CONTROL_ABI = 32'h10203040,
    parameter integer MEMORY_BACKEND = 0,
    parameter [31:0] DDR_BASE = 32'd0,
    parameter [31:0] DDR_BYTES = 32'd0
) (
    input wire clk, reset_n,
    input wire [7:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata,
    output reg readdatavalid, writeresponsevalid,
    output reg [1:0] response,
    output wire waitrequest,
    output wire ddr_arvalid,
    output wire [31:0] ddr_araddr,
    output wire [7:0] ddr_arlen,
    output wire [2:0] ddr_arsize,
    output wire [1:0] ddr_arburst,
    output wire [3:0] ddr_arid,
    output wire ddr_arlock,
    output wire [3:0] ddr_arcache,
    output wire [2:0] ddr_arprot,
    output wire [3:0] ddr_arqos,
    input wire ddr_arready,
    input wire ddr_rvalid,
    input wire [127:0] ddr_rdata,
    input wire ddr_rlast,
    input wire [1:0] ddr_rresp,
    input wire [3:0] ddr_rid,
    output wire ddr_rready
);
    localparam DDR_MODE = MEMORY_BACKEND == 1;
    localparam [31:0] ENDPOINT_ABI = DDR_MODE ? CONTROL_ABI ^ 32'd1 : CONTROL_ABI;
    localparam [7:0] DIAG_COMMAND = 8'h80, DIAG_ADDRESS = 8'h84,
                     DIAG_LENGTH = 8'h88, DIAG_STATUS = 8'h8c,
                     DIAG_HELD_ADDRESS = 8'h90, DIAG_BYTES = 8'h94,
                     DIAG_CHECKSUM = 8'h98, DIAG_DATA0 = 8'ha0,
                     DIAG_DATA1 = 8'ha4, DIAG_DATA2 = 8'ha8,
                     DIAG_DATA3 = 8'hac, DIAG_BASE = 8'hb0,
                     DIAG_SPAN = 8'hb4;
    reg busy, pending_read, pending_write, pending_local, local_error;
    reg [31:0] local_data;
    wire request = read || write;
    wire embedded_request = address < DIAG_COMMAND && !(read && write);
    wire csr_ready, csr_response_valid, csr_error;
    wire [31:0] csr_rdata;
    assign waitrequest = !reset_n || busy || (request && embedded_request && !csr_ready);
    wire accept = request && !waitrequest;

    reg [31:0] staged_address;
    reg [7:0] staged_length;
    reg [31:0] offered_address;
    reg [7:0] offered_length;
    reg allow_address, allow_reader, reader_valid;
    wire reader_ready, reader_abort, reader_rvalid, reader_rlast;
    wire [127:0] reader_rdata;
    wire m_arvalid, m_rready;
    wire [31:0] m_araddr;
    wire [7:0] m_arlen;
    wire [2:0] m_arsize;
    wire [1:0] m_arburst;
    wire [3:0] m_arid;
    reg memory_active, memory_valid;
    reg [8:0] beats_left;
    reg [1:0] memory_response;
    reg [31:0] returned_bytes, checksum;
    reg [127:0] last_data;
    wire m_arready = DDR_MODE ? ddr_arready : allow_address && !memory_active;
    wire memory_last = beats_left == 9'd1;
    wire response_valid = DDR_MODE ? ddr_rvalid : memory_valid;
    wire [127:0] response_data = DDR_MODE ? ddr_rdata : {4{32'hde250001}};
    wire response_last = DDR_MODE ? ddr_rlast : memory_last;
    wire [1:0] response_code = DDR_MODE ? ddr_rresp : memory_response;
    wire [3:0] response_id = DDR_MODE ? ddr_rid : 4'd0;
    wire [32:0] window_end = {1'b0,DDR_BASE} + {1'b0,DDR_BYTES};
    wire [32:0] request_bytes = ({25'd0,staged_length} + 33'd1) << 4;
    wire [32:0] request_end = {1'b0,staged_address} + request_bytes;
    wire ddr_descriptor_valid = DDR_BYTES != 0 && DDR_BASE[3:0] == 0 && DDR_BYTES[3:0] == 0 &&
        window_end <= 33'h100000000 && staged_address[3:0] == 0 &&
        staged_address >= DDR_BASE && request_end <= window_end &&
        {21'd0,staged_address[11:0]} + request_bytes <= 33'd4096;
    // VALID, address and length are held by the control endpoint. Do not gate
    // a real address offer with a software-controlled READY substitute.
    assign ddr_arvalid = DDR_MODE && m_arvalid;
    assign ddr_araddr = m_araddr;
    assign ddr_arlen = m_arlen;
    assign ddr_arsize = m_arsize;
    assign ddr_arburst = m_arburst;
    assign ddr_arid = m_arid;
    assign ddr_arlock = 1'b0;
    assign ddr_arcache = 4'd0;
    assign ddr_arprot = 3'b010; // Non-secure data read; no instruction access.
    assign ddr_arqos = 4'd0;
    assign ddr_rready = DDR_MODE && m_rready;
    wire [31:0] status = {7'd0, beats_left, 8'd0, reader_rvalid, m_rready,
                          reader_abort, response_valid, memory_active,
                          m_arvalid, reader_valid, reader_ready};

    fes_embedded_control #(.BUILD_ID(BUILD_ID), .ABI_ID(ENDPOINT_ABI), .DATA_WIDTH(128)) control (
        .clk(clk), .reset_n(reset_n),
        .csr_valid(accept && embedded_request), .csr_write(write), .csr_address(address),
        .csr_wdata(writedata), .csr_wstrb(byteenable), .csr_ready(csr_ready),
        .csr_response_valid(csr_response_valid), .csr_error(csr_error), .csr_rdata(csr_rdata),
        .csr_response_ready(1'b1),
        .reader_valid(reader_valid), .reader_address(offered_address), .reader_length(offered_length),
        .reader_ready(reader_ready), .reader_rvalid(reader_rvalid), .reader_rdata(reader_rdata),
        .reader_rlast(reader_rlast), .reader_rready(allow_reader), .reader_abort(reader_abort),
        .m_arvalid(m_arvalid), .m_araddr(m_araddr), .m_arlen(m_arlen), .m_arsize(m_arsize),
        .m_arburst(m_arburst), .m_arid(m_arid), .m_arready(m_arready),
        .m_rvalid(response_valid), .m_rdata(response_data), .m_rlast(response_last),
        .m_rresp(response_code), .m_rid(response_id), .m_rready(m_rready)
    );

    always @(posedge clk) begin
        if (!reset_n) begin
            busy <= 0; pending_read <= 0; pending_write <= 0; pending_local <= 0;
            local_error <= 0; local_data <= 0;
            readdata <= 0; readdatavalid <= 0; writeresponsevalid <= 0; response <= 0;
            staged_address <= 0; staged_length <= 0; allow_address <= 0; allow_reader <= 0;
            offered_address <= 0; offered_length <= 0;
            reader_valid <= 0; memory_active <= 0; memory_valid <= 0;
            beats_left <= 0; memory_response <= 0;
            returned_bytes <= 0; checksum <= 0; last_data <= 0;
        end else begin
            readdatavalid <= 0; writeresponsevalid <= 0;
            if (reader_valid && (reader_ready || reader_abort)) reader_valid <= 0;
            if (m_arvalid && m_arready) begin
                memory_active <= 1;
                beats_left <= {1'b0, m_arlen} + 9'd1;
            end
            if (response_valid && m_rready) begin
                memory_valid <= 0;
                returned_bytes <= returned_bytes + 32'd16;
                checksum <= checksum ^ response_data[31:0] ^ response_data[63:32] ^
                            response_data[95:64] ^ response_data[127:96];
                last_data <= response_data;
                if (memory_active && memory_last) begin memory_active <= 0; beats_left <= 0; end
                else if (memory_active) beats_left <= beats_left - 9'd1;
            end
            if (busy && (pending_local || csr_response_valid)) begin
                busy <= 0;
                readdatavalid <= pending_read;
                writeresponsevalid <= pending_write;
                readdata <= pending_local ? local_data : csr_rdata;
                response <= (pending_local ? local_error : csr_error) ? 2'b10 : 2'b00;
            end
            if (accept) begin
                busy <= 1; pending_read <= read; pending_write <= write;
                pending_local <= !embedded_request; local_error <= 0; local_data <= 0;
                if (!embedded_request) begin
                    if ((read && write) || address[1:0] != 0) local_error <= 1;
                    else if (read) begin
                        case (address)
                            DIAG_COMMAND: local_data <= {29'd0, allow_reader, 1'b0, allow_address};
                            DIAG_ADDRESS: local_data <= staged_address;
                            DIAG_LENGTH: local_data <= {24'd0, staged_length};
                            DIAG_STATUS: local_data <= status;
                            DIAG_HELD_ADDRESS: local_data <= m_araddr;
                            DIAG_BYTES: local_data <= returned_bytes;
                            DIAG_CHECKSUM: local_data <= checksum;
                            DIAG_DATA0: local_data <= last_data[31:0];
                            DIAG_DATA1: local_data <= last_data[63:32];
                            DIAG_DATA2: local_data <= last_data[95:64];
                            DIAG_DATA3: local_data <= last_data[127:96];
                            DIAG_BASE: local_data <= DDR_MODE ? DDR_BASE : 32'd0;
                            DIAG_SPAN: local_data <= DDR_MODE ? DDR_BYTES : 32'd0;
                            default: local_error <= 1;
                        endcase
                    end else if (byteenable != 4'hf) local_error <= 1;
                    else case (address)
                        DIAG_ADDRESS: staged_address <= writedata;
                        DIAG_LENGTH: begin
                            if (writedata > 32'd255) local_error <= 1;
                            else staged_length <= writedata[7:0];
                        end
                        DIAG_COMMAND: begin
                            // bit 0: accept an AXI address; bit 2: reader ready;
                            // bit 3: offer one read; bit 1: return one beat;
                            // bit 4: inject a bad response on that beat.
                            if (writedata[31:5] != 0 || (writedata[4] && !writedata[1]) ||
                                (writedata[3] && (!reader_ready || reader_valid)) ||
                                (writedata[1] && (!memory_active || memory_valid)) ||
                                (DDR_MODE && (|(writedata & ~32'hc) ||
                                 (writedata[3] && (!ddr_descriptor_valid || memory_active || m_arvalid))))) local_error <= 1;
                            else begin
                                allow_address <= writedata[0]; allow_reader <= writedata[2];
                                if (writedata[3]) begin
                                    reader_valid <= 1;
                                    offered_address <= staged_address; offered_length <= staged_length;
                                    if (DDR_MODE) begin returned_bytes <= 0; checksum <= 0; last_data <= 0; end
                                end
                                if (writedata[1]) begin
                                    memory_valid <= 1;
                                    memory_response <= writedata[4] ? 2'b10 : 2'b00;
                                end
                            end
                        end
                        default: local_error <= 1;
                    endcase
                end
            end
        end
    end
endmodule
