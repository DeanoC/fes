// SPDX-License-Identifier: GPL-3.0-or-later
// Diagnostic Avalon aperture and bounded on-chip read responder. This does
// not connect the reader to DDR or contain any other FPGA master.
`ifndef FES_DE25_COMPUTER_BUILD_ID
`define FES_DE25_COMPUTER_BUILD_ID 128'h0
`endif

module fes_de25_embedded #(
    parameter [127:0] BUILD_ID = `FES_DE25_COMPUTER_BUILD_ID,
    parameter [31:0] CONTROL_ABI = 32'h10203040
) (
    input wire clk, reset_n,
    input wire [7:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata,
    output reg readdatavalid, writeresponsevalid,
    output reg [1:0] response,
    output wire waitrequest
);
    localparam [7:0] DIAG_COMMAND = 8'h80, DIAG_ADDRESS = 8'h84,
                     DIAG_LENGTH = 8'h88, DIAG_STATUS = 8'h8c,
                     DIAG_HELD_ADDRESS = 8'h90;
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
    wire m_arready = allow_address && !memory_active;
    wire memory_last = beats_left == 9'd1;
    wire [31:0] status = {7'd0, beats_left, 8'd0, reader_rvalid, m_rready,
                          reader_abort, memory_valid, memory_active,
                          m_arvalid, reader_valid, reader_ready};

    fes_embedded_control #(.BUILD_ID(BUILD_ID), .ABI_ID(CONTROL_ABI), .DATA_WIDTH(128)) control (
        .clk(clk), .reset_n(reset_n),
        .csr_valid(accept && embedded_request), .csr_write(write), .csr_address(address),
        .csr_wdata(writedata), .csr_wstrb(byteenable), .csr_ready(csr_ready),
        .csr_response_valid(csr_response_valid), .csr_error(csr_error), .csr_rdata(csr_rdata),
        .csr_response_ready(1'b1),
        .reader_valid(reader_valid), .reader_address(staged_address), .reader_length(staged_length),
        .reader_ready(reader_ready), .reader_rvalid(reader_rvalid), .reader_rdata(reader_rdata),
        .reader_rlast(reader_rlast), .reader_rready(allow_reader), .reader_abort(reader_abort),
        .m_arvalid(m_arvalid), .m_araddr(m_araddr), .m_arlen(m_arlen), .m_arsize(m_arsize),
        .m_arburst(m_arburst), .m_arid(m_arid), .m_arready(m_arready),
        .m_rvalid(memory_valid), .m_rdata({4{32'hde250001}}), .m_rlast(memory_last),
        .m_rresp(memory_response), .m_rid(4'd0), .m_rready(m_rready)
    );

    always @(posedge clk) begin
        if (!reset_n) begin
            busy <= 0; pending_read <= 0; pending_write <= 0; pending_local <= 0;
            local_error <= 0; local_data <= 0;
            readdata <= 0; readdatavalid <= 0; writeresponsevalid <= 0; response <= 0;
            staged_address <= 0; staged_length <= 0; allow_address <= 0; allow_reader <= 0;
            reader_valid <= 0; memory_active <= 0; memory_valid <= 0;
            beats_left <= 0; memory_response <= 0;
        end else begin
            readdatavalid <= 0; writeresponsevalid <= 0;
            if (reader_valid && (reader_ready || reader_abort)) reader_valid <= 0;
            if (m_arvalid && m_arready) begin
                memory_active <= 1;
                beats_left <= {1'b0, m_arlen} + 9'd1;
            end
            if (memory_valid && m_rready) begin
                memory_valid <= 0;
                if (memory_last) begin memory_active <= 0; beats_left <= 0; end
                else beats_left <= beats_left - 9'd1;
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
                                (writedata[1] && (!memory_active || memory_valid))) local_error <= 1;
                            else begin
                                allow_address <= writedata[0]; allow_reader <= writedata[2];
                                if (writedata[3]) reader_valid <= 1;
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
