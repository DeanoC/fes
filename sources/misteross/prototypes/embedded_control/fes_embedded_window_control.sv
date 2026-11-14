// SPDX-License-Identifier: GPL-3.0-or-later
// CPU-started fixed-window read diagnostic. The sink checks bytes with CRC32;
// a completed frame is neither a reader drain nor a display/HDMI receipt.
module fes_embedded_window_control #(
    parameter [31:0] WINDOW_BASE = 32'h95000000,
    parameter [31:0] WINDOW_BYTES = 32'd4194304,
    parameter [31:0] ROW_BYTES = 32'd5120,
    parameter [31:0] STRIDE_BYTES = 32'd5120,
    parameter [31:0] ROWS = 32'd720
)(
    input wire clk, reset_n, maintenance_held,
    input wire valid, write,
    input wire [7:0] address,
    input wire [31:0] wdata,
    input wire [3:0] wstrb,
    output reg [31:0] rdata,
    output reg error,
    output wire reader_valid,
    output wire [31:0] reader_address,
    output wire [7:0] reader_length,
    input wire reader_ready, reader_abort,
    input wire [63:0] reader_generation,
    input wire reader_rvalid,
    input wire [127:0] reader_rdata,
    output wire reader_rready
);
    localparam [7:0] COMMAND=8'h80, EXPECT_LO=8'h84, EXPECT_HI=8'h88,
        COOKIE=8'h8c, STATUS=8'h90, RESULT_LO=8'h94, RESULT_HI=8'h98,
        RESULT_COOKIE=8'h9c, REQUESTED=8'ha0, DELIVERED=8'ha4, CRC=8'ha8,
        BASE=8'hb0, SPAN=8'hb4, ROW_SIZE=8'hb8, STRIDE=8'hbc,
        HEIGHT=8'hc0, MAGIC=8'hc4, MAINTENANCE_HELD=8'hc8;
    reg [63:0] expected_generation, frame_generation, snapshot_generation;
    reg [31:0] staged_cookie, frame_cookie, snapshot_cookie;
    reg [31:0] snapshot_requested, snapshot_delivered, snapshot_crc;
    reg [31:0] crc;
    reg done;
    wire ready, busy, complete, aborted, fault;
    wire [31:0] requested_bytes, delivered_bytes;
    wire stream_valid;
    wire [127:0] stream_data;
    wire newer_cookie = staged_cookie != 0 &&
        (expected_generation != frame_generation || staged_cookie > frame_cookie);
    wire start_allowed = ready && expected_generation != 0 &&
        expected_generation == reader_generation && newer_cookie;
    wire start = valid && write && address == COMMAND && wstrb == 4'hf &&
        wdata == 32'd1 && start_allowed;
    wire owned_done = (done || complete) && !reader_abort && !fault && reader_generation == frame_generation;
    wire [31:0] flags = {26'd0,reader_abort,fault,aborted,owned_done,busy,ready};

    // IEEE reflected CRC32 in memory byte order, initial/final XOR ffffffff.
    // This is a diagnostic sink, with no independent AXI master or write path.
    function automatic [31:0] next_crc(input [31:0] previous, input [127:0] data);
        reg [31:0] value;
        integer bit_index;
        begin
            value = previous;
            for (bit_index=0;bit_index<128;bit_index=bit_index+1)
                value = (value >> 1) ^ ((value[0] ^ data[bit_index]) ? 32'hedb88320 : 32'd0);
            next_crc = value;
        end
    endfunction

    fes_embedded_window_reader #(.WINDOW_BASE(WINDOW_BASE),.WINDOW_BYTES(WINDOW_BYTES),
        .ROW_BYTES(ROW_BYTES),.STRIDE_BYTES(STRIDE_BYTES),.ROWS(ROWS)) source (
        .clk(clk),.reset_n(reset_n),.start(start),.start_ready(ready),.busy(busy),
        .complete(complete),.aborted(aborted),.fault(fault),
        .requested_bytes(requested_bytes),.delivered_bytes(delivered_bytes),
        .reader_valid(reader_valid),.reader_address(reader_address),.reader_length(reader_length),
        .reader_ready(reader_ready),.reader_abort(reader_abort),.reader_generation(reader_generation),
        .reader_rvalid(reader_rvalid),.reader_rdata(reader_rdata),.reader_rready(reader_rready),
        .stream_valid(stream_valid),.stream_data(stream_data),.stream_ready(1'b1),
        /* verilator lint_off PINCONNECTEMPTY */
        .stream_first(),.stream_row_last(),.stream_last()
        /* verilator lint_on PINCONNECTEMPTY */
    );

    always @* begin
        rdata=0; error=0;
        if (valid) begin
            if (address[1:0]!=0 || wstrb!=4'hf) error=1;
            else if (write) begin
                case (address)
                    EXPECT_LO,EXPECT_HI,COOKIE: if (busy) error=1;
                    COMMAND: if (wdata!=32'd1 || !start_allowed) error=1;
                    default: error=1;
                endcase
            end else case (address)
                COMMAND: rdata=0;
                EXPECT_LO: rdata=expected_generation[31:0];
                EXPECT_HI: rdata=expected_generation[63:32];
                COOKIE: rdata=staged_cookie;
                // STATUS captures the following result registers together.
                // Reading the result later cannot mix counts from two frames.
                STATUS: rdata=flags;
                RESULT_LO: rdata=snapshot_generation[31:0];
                RESULT_HI: rdata=snapshot_generation[63:32];
                RESULT_COOKIE: rdata=snapshot_cookie;
                REQUESTED: rdata=snapshot_requested;
                DELIVERED: rdata=snapshot_delivered;
                CRC: rdata=snapshot_crc;
                BASE: rdata=WINDOW_BASE;
                SPAN: rdata=WINDOW_BYTES;
                ROW_SIZE: rdata=ROW_BYTES;
                STRIDE: rdata=STRIDE_BYTES;
                HEIGHT: rdata=ROWS;
                MAGIC: rdata=32'h31435746; // FWC1
                MAINTENANCE_HELD: rdata={31'd0,maintenance_held};
                default: error=1;
            endcase
        end
    end

    always @(posedge clk) begin
        if (!reset_n) begin
            expected_generation<=0; staged_cookie<=0;
            frame_generation<=0; frame_cookie<=0; done<=0; crc<=32'hffffffff;
            snapshot_generation<=0; snapshot_cookie<=0; snapshot_requested<=0;
            snapshot_delivered<=0; snapshot_crc<=0;
        end else begin
            if (start) begin
                frame_generation<=expected_generation; frame_cookie<=staged_cookie;
                done<=0; crc<=32'hffffffff;
            end else begin
                if (stream_valid) crc<=next_crc(crc,stream_data);
                if (complete) done<=1;
                if (reader_abort || fault || reader_generation!=frame_generation) done<=0;
            end
            if (valid && !error && write) case (address)
                EXPECT_LO: expected_generation[31:0]<=wdata;
                EXPECT_HI: expected_generation[63:32]<=wdata;
                COOKIE: staged_cookie<=wdata;
                default: ;
            endcase
            if (valid && !error && !write && address==STATUS) begin
                snapshot_generation<=frame_generation; snapshot_cookie<=frame_cookie;
                snapshot_requested<=requested_bytes; snapshot_delivered<=delivered_bytes;
                snapshot_crc<=crc ^ 32'hffffffff;
            end
        end
    end
endmodule
