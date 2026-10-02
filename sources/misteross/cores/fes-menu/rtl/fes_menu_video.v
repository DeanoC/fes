// SPDX-License-Identifier: GPL-2.0-or-later
// Fixed timing never waits for memory. Fetch a complete frame's ordered stream
// with initial prefetch in vertical blank; late pixels are discarded by index.
module fes_menu_video #(
    parameter [31:0] WINDOW_BASE = 32'd0,
    parameter [0:0] TEST_PATTERN = 1'b0,
    // Session planes share the machine's raster and acknowledge only a fully
    // scanned frame. Defaults preserve the standalone idle-menu contract.
    parameter [0:0] EXTERNAL_RASTER = 1'b0,
    parameter [0:0] COMPLETE_FRAME_ACK = 1'b0
) (
    input wire clk, rst, enable, quiesce,
    input wire [10:0] raster_h,
    input wire [9:0] raster_v,
    input wire submit_valid, submit_slot,
    input wire [31:0] submit_sequence,
    output wire submit_ready,
    output reg [31:0] displayed_sequence = 32'd0,
    output wire quiesced,
    output reg [31:0] underflows = 32'd0,
    output wire [23:0] rgb,
    output wire de, hs, vs,
    output wire [27:0] address,
    output wire [7:0] burstcount,
    output wire read,
    input wire waitrequest,
    input wire [127:0] readdata,
    input wire readdatavalid
);
    localparam [1:0] DRAIN = 2'd0, START = 2'd1, FETCH = 2'd2, DISPLAY = 2'd3;
    reg [1:0] state = DRAIN;
    reg [10:0] internal_h = 11'd0;
    reg [9:0] internal_v = 10'd720;
    wire [10:0] h = EXTERNAL_RASTER ? raster_h : internal_h;
    wire [9:0] v = EXTERNAL_RASTER ? raster_v : internal_v;
    reg frame_complete = 1'b0;
    reg [19:0] raster_index = 20'd0;
    reg pending = 1'b0;
    reg armed = 1'b0;
    reg pending_slot = 1'b0;
    reg [31:0] pending_sequence = 32'd0;
    reg current_slot = 1'b0;
    reg fetch_slot = 1'b0;
    reg [31:0] fetch_sequence = 32'd0;
    wire running = enable && !quiesce && !rst;
    wire frame_end = h == 11'd1649 && v == 10'd749;
    wire blank_start = h == 11'd1279 && v == 10'd719;
    wire reader_idle;
    wire pixel_valid;
    wire [31:0] pixel_bgrx;
    wire [19:0] pixel_index;
    wire reader_enable = running && state != DRAIN;
    wire pixel_matches = state == DISPLAY && pixel_valid && pixel_index == raster_index;
    // Blank intervals discard only missed pixels; future-row pixels stay queued.
    wire pixel_ready = state == DISPLAY &&
        (de ? pixel_index <= raster_index : pixel_index < raster_index);
    assign de = h < 11'd1280 && v < 10'd720;
    assign hs = h >= 11'd1390 && h < 11'd1430;
    assign vs = v >= 10'd725 && v < 10'd730;
    assign rgb = de && running && pixel_matches ? pixel_bgrx[23:0] : 24'd0;
    assign quiesced = (!enable || quiesce) && reader_idle && state == DRAIN;
    assign submit_ready = running && !pending && (state == DISPLAY || state == DRAIN);

    wire [27:0] reader_address;
    wire [7:0] reader_burstcount;
    wire reader_read, reader_waitrequest, reader_readdatavalid;
    wire [127:0] reader_readdata;
    generate if (TEST_PATTERN) begin : pattern_source
        fes_menu_pattern_memory memory (
            .clk(clk), .address(reader_address), .burstcount(reader_burstcount),
            .read(reader_read), .waitrequest(reader_waitrequest),
            .readdata(reader_readdata), .readdatavalid(reader_readdatavalid)
        );
        assign address = 28'd0;
        assign burstcount = 8'd0;
        assign read = 1'b0;
    end else begin : ddr_source
        assign address = reader_address;
        assign burstcount = reader_burstcount;
        assign read = reader_read;
        assign reader_waitrequest = waitrequest;
        assign reader_readdata = readdata;
        assign reader_readdatavalid = readdatavalid;
    end endgenerate

    fes_menu_reader #(.WINDOW_BASE(TEST_PATTERN ? 32'h00001000 : WINDOW_BASE)) reader (
        .clk(clk), .rst(rst), .enable(reader_enable), .slot(fetch_slot),
        .start(state == START && running), .pixel_ready(pixel_ready),
        .pixel_valid(pixel_valid), .pixel_bgrx(pixel_bgrx),
        .pixel_index(pixel_index), .done(), .idle(reader_idle),
        .address(reader_address), .burstcount(reader_burstcount), .read(reader_read),
        .waitrequest(reader_waitrequest), .readdata(reader_readdata),
        .readdatavalid(reader_readdatavalid)
    );
    always @(posedge clk) begin
        if (!EXTERNAL_RASTER) begin
            if (internal_h == 11'd1649) begin
                internal_h <= 11'd0;
                internal_v <= internal_v == 10'd749 ? 10'd0 : internal_v + 10'd1;
            end else internal_h <= internal_h + 11'd1;
        end
        if (frame_end) raster_index <= 20'd0;
        else if (de) raster_index <= raster_index + 20'd1;

        if (submit_valid && submit_ready) begin
            pending <= 1'b1;
            pending_slot <= submit_slot;
            pending_sequence <= submit_sequence;
        end
        if (running) begin
            case (state)
                DRAIN: if (h < 11'd1280 && v == 10'd720 && reader_idle) begin
                    // Start only in the first blank row, with 29 rows to prefetch.
                    armed <= 1'b1;
                    fetch_slot <= pending ? pending_slot :
                        (submit_valid && submit_ready ? submit_slot : current_slot);
                    fetch_sequence <= pending ? pending_sequence :
                        (submit_valid && submit_ready ? submit_sequence : displayed_sequence);
                    state <= START;
                end
                START: state <= FETCH;
                FETCH: if (frame_end) begin
                    if (pixel_valid && pixel_index == 20'd0) begin
                        state <= DISPLAY;
                        current_slot <= fetch_slot;
                        frame_complete <= 1'b1;
                        if (!COMPLETE_FRAME_ACK) begin
                            displayed_sequence <= fetch_sequence;
                            pending <= 1'b0;
                        end
                    end else state <= DRAIN;
                end
                DISPLAY: begin
                    if (de && !pixel_matches) frame_complete <= 1'b0;
                    if (blank_start) begin
                        state <= DRAIN;
                        // Include the final pixel in the completion decision.
                        if (COMPLETE_FRAME_ACK && frame_complete && pixel_matches) begin
                            displayed_sequence <= fetch_sequence;
                            // A new submission received during this frame
                            // belongs to the next fetch, not this completion.
                            if (pending_sequence == fetch_sequence) pending <= 1'b0;
                        end
                    end
                end
                default: state <= DRAIN;
            endcase
        end else begin
            state <= DRAIN;
            pending <= 1'b0;
            armed <= 1'b0;
        end
        if (de && armed && running && !pixel_matches && underflows != 32'hffffffff)
            underflows <= underflows + 32'd1;
        if (rst) begin
            internal_h <= 11'd0;
            internal_v <= 10'd720;
            frame_complete <= 1'b0;
            raster_index <= 20'd0;
            displayed_sequence <= 32'd0;
            underflows <= 32'd0;
            armed <= 1'b0;
            current_slot <= 1'b0;
        end
    end
endmodule
