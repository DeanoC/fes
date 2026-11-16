// SPDX-License-Identifier: GPL-3.0-or-later
`include "fes_embedded_rom.vh"
// Peripheral-relative CPU-fed ROM endpoint. It initiates no memory accesses.
// The enclosing platform supplies the sealed identity and owned CPU Hold.
module fes_embedded_rom_csr #(
    parameter integer ROM_BYTES = 8192,
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire clk, reset_n, cpu_held,
    input wire csr_valid, csr_write,
    input wire [6:0] csr_address,
    input wire [31:0] csr_wdata,
    input wire [3:0] csr_wstrb,
    output reg [31:0] csr_rdata,
    output wire csr_error,
    output wire loaded, failed,
    output wire [13:0] received, written,
    output wire [31:0] observed_crc,
    input wire [12:0] read_address,
    output wire [7:0] read_data
);
    wire [31:0] offset = {25'd0,csr_address};
    reg started, crc_written, fault, pending_valid;
    reg [2:0] next_word;
    reg [127:0] staged;
    reg [31:0] expected_crc;
    wire sink_loaded, sink_failed, sink_ready;
    reg bad_access;
    wire is_data = offset >= `FES_EMBEDDED_ROM_DATA_OFFSET && offset < `FES_EMBEDDED_ROM_DATA_OFFSET + 16;
    // Use registered progress for access validation: sink_ready is qualified
    // by abort_load, which itself includes a rejected CSR transaction.
    wire can_stage = started && cpu_held && received == written && !sink_loaded && !sink_failed && !pending_valid && !fault;
    wire input_ready = can_stage && next_word == 0;
    wire start_command = csr_valid && csr_write && offset == `FES_EMBEDDED_ROM_COMMAND_OFFSET &&
                         csr_wdata == `FES_EMBEDDED_ROM_START && !bad_access;
    wire abort_command = csr_valid && csr_write && offset == `FES_EMBEDDED_ROM_COMMAND_OFFSET &&
                         csr_wdata == `FES_EMBEDDED_ROM_ABORT;
    assign csr_error = csr_valid && bad_access;
    assign failed = fault || sink_failed;
    assign loaded = sink_loaded && !failed && reset_n;

    always @* begin
        csr_rdata = 0;
        bad_access = csr_address[1:0] != 0;
        if (csr_write) begin
            if (csr_wstrb != 4'hf || fault || sink_failed) bad_access = 1;
            case (offset)
                `FES_EMBEDDED_ROM_EXPECTED_CRC_OFFSET:
                    if (started || crc_written || !cpu_held) bad_access = 1;
                `FES_EMBEDDED_ROM_COMMAND_OFFSET:
                    if (csr_wdata == `FES_EMBEDDED_ROM_START) begin
                        if (started || !crc_written || !cpu_held || received != 0 || written != 0) bad_access = 1;
                    end else if (csr_wdata != `FES_EMBEDDED_ROM_ABORT) bad_access = 1;
                default:
                    if (!is_data || !can_stage ||
                        next_word != {1'b0,csr_address[3:2]}) bad_access = 1;
            endcase
        end else begin
            case (offset)
                `FES_EMBEDDED_ROM_ID_OFFSET: csr_rdata = `FES_EMBEDDED_ROM_MAGIC;
                `FES_EMBEDDED_ROM_VERSION_OFFSET: csr_rdata = `FES_EMBEDDED_ROM_VERSION;
                `FES_EMBEDDED_ROM_BYTES_OFFSET: csr_rdata = 32'(ROM_BYTES);
                `FES_EMBEDDED_ROM_STATUS_OFFSET:
                    csr_rdata = (cpu_held ? `FES_EMBEDDED_ROM_HELD : 0) |
                        (input_ready ? `FES_EMBEDDED_ROM_READY : 0) |
                        (loaded ? `FES_EMBEDDED_ROM_LOADED : 0) |
                        (failed ? `FES_EMBEDDED_ROM_FAILED : 0) |
                        (crc_written && !loaded && !failed ? `FES_EMBEDDED_ROM_ACTIVE : 0);
                `FES_EMBEDDED_ROM_EXPECTED_CRC_OFFSET: csr_rdata = expected_crc;
                `FES_EMBEDDED_ROM_OBSERVED_CRC_OFFSET: csr_rdata = observed_crc;
                `FES_EMBEDDED_ROM_RECEIVED_OFFSET: csr_rdata = {18'd0,received};
                `FES_EMBEDDED_ROM_WRITTEN_OFFSET: csr_rdata = {18'd0,written};
                `FES_EMBEDDED_ROM_BUILD_OFFSET: csr_rdata = BUILD_ID[127:96];
                `FES_EMBEDDED_ROM_BUILD_OFFSET+4: csr_rdata = BUILD_ID[95:64];
                `FES_EMBEDDED_ROM_BUILD_OFFSET+8: csr_rdata = BUILD_ID[63:32];
                `FES_EMBEDDED_ROM_BUILD_OFFSET+12: csr_rdata = BUILD_ID[31:0];
                default: bad_access = 1;
            endcase
        end
        if (!csr_valid) bad_access = 0;
    end

    fes_embedded_rom #(.ROM_BYTES(ROM_BYTES)) sink (
        .clk(clk),.reset_n(reset_n),.cpu_held(cpu_held),.start(start_command),
        .abort_load(fault || bad_access || abort_command),.expected_crc32(expected_crc),
        .stream_valid(pending_valid && !fault && !bad_access),.stream_ready(sink_ready),
        .stream_data(staged),.stream_last(received + 14'd16 == 14'(ROM_BYTES)),
        .loaded(sink_loaded),.failed(sink_failed),.received(received),.written(written),
        .observed_crc32(observed_crc),.read_address(read_address),.read_data(read_data)
    );
    always @(posedge clk) begin
        if (!reset_n) begin
            started <= 0; crc_written <= 0; fault <= 0;
            pending_valid <= 0; next_word <= 0; staged <= 0; expected_crc <= 0;
        end else begin
            if (bad_access || abort_command) fault <= 1;
            if (pending_valid && sink_ready) pending_valid <= 0;
            if (csr_valid && csr_write && !bad_access) begin
                if (offset == `FES_EMBEDDED_ROM_EXPECTED_CRC_OFFSET) begin
                    expected_crc <= csr_wdata; crc_written <= 1;
                end else if (start_command) started <= 1;
                else if (is_data) begin
                    staged[32*csr_address[3:2] +: 32] <= csr_wdata;
                    if (next_word == 3) begin next_word <= 0; pending_valid <= 1; end
                    else next_word <= next_word + 1;
                end
            end
        end
    end
endmodule
