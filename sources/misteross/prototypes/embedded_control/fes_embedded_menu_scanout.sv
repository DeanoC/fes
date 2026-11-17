// SPDX-License-Identifier: GPL-3.0-or-later
// Owned 720p menu scanout. All ports use the same qualified pixel clock.
// Board integration must supply clock crossings, physical DDR and HDMI glue.
// Local stop/completion never substitutes for the endpoint/all-master drain.
module fes_embedded_menu_scanout #(
    parameter [31:0] WINDOW_BASE = 32'h95000000,
    parameter [31:0] WINDOW_BYTES = 32'd8388608
)(
    input wire clk, reset_n,
    input wire start, initial_slot,
    input wire [31:0] initial_sequence,
    output wire start_ready,
    input wire submit_valid, submit_slot,
    input wire [31:0] submit_sequence,
    output wire submit_ready,
    input wire quiesce,
    output reg running, stopped, fault,
    output wire [31:0] displayed_sequence, underflows,
    output wire [23:0] rgb,
    output wire de, hs, vs,
    output wire reader_valid,
    output wire [31:0] reader_address,
    output wire [7:0] reader_length,
    input wire reader_ready, reader_abort,
    input wire [63:0] reader_generation,
    input wire reader_rvalid,
    input wire [127:0] reader_rdata,
    output wire reader_rready
);
    // The existing menu reader selects 4 MiB slots and 720 packed XRGB rows.
    localparam CONFIG_VALID = WINDOW_BASE != 0 && WINDOW_BASE[21:0] == 0 &&
        WINDOW_BYTES == 32'd8388608 &&
        {1'b0, WINDOW_BASE} + {1'b0, WINDOW_BYTES} <= 33'h100000000;
    reg saw_retirement, stop_requested, initial_pending;
    reg [63:0] retired_generation, active_generation;
    reg first_slot, last_slot;
    reg [31:0] first_sequence, last_sequence;
    wire owned = reader_generation == active_generation && !reader_abort;
    wire video_ready, video_stopped;
    wire [27:0] address;
    wire [7:0] burstcount;
    wire read;
    wire [7:0] unused_alpha;
    wire stopping = stop_requested || quiesce;
    wire video_enable = reset_n && running && !fault && owned;
    wire valid_submission = submit_sequence > last_sequence && submit_slot != last_slot;
    assign start_ready = reset_n && CONFIG_VALID && !running && !fault &&
        !reader_abort && saw_retirement && reader_generation > retired_generation;
    assign submit_ready = video_enable && !stopping && !initial_pending && video_ready;
    assign reader_valid = video_enable && read;
    assign reader_address = {address, 4'b0000};
    assign reader_length = burstcount - 8'd1;
    // The existing menu FIFO reserves a complete burst before requesting it.
    // It consumes every returned beat, including during local quiescence.
    assign reader_rready = 1'b1;

    fes_menu_video #(.WINDOW_BASE(WINDOW_BASE), .COMPLETE_FRAME_ACK(1'b1)) video (
        .clk(clk), .rst(!reset_n), .enable(video_enable), .quiesce(stopping),
        .raster_h(11'd0), .raster_v(10'd0),
        .submit_valid(initial_pending || (submit_valid && valid_submission && !stopping)),
        .submit_slot(initial_pending ? first_slot : submit_slot),
        .submit_sequence(initial_pending ? first_sequence : submit_sequence),
        .submit_ready(video_ready), .displayed_sequence(displayed_sequence),
        .quiesced(video_stopped), .underflows(underflows),
        .rgb(rgb), .alpha(unused_alpha), .de(de), .hs(hs), .vs(vs),
        .address(address), .burstcount(burstcount), .read(read),
        .waitrequest(!reader_ready), .readdata(reader_rdata),
        .readdatavalid(reader_rvalid)
    );

    always @(posedge clk) begin
        if (!reset_n) begin
            running <= 0; stopped <= 0; fault <= !CONFIG_VALID;
            saw_retirement <= 0; retired_generation <= 0; active_generation <= 0;
            stop_requested <= 0; initial_pending <= 0;
            first_slot <= 0; last_slot <= 0; first_sequence <= 0; last_sequence <= 0;
        end else begin
            if (reader_abort) begin
                saw_retirement <= 1;
                retired_generation <= reader_generation;
            end
            if (running && !owned) begin
                // Ownership loss is terminal, even if a new generation appears.
                fault <= 1;
            end
            if (start) begin
                if (!start_ready || initial_sequence <= last_sequence || quiesce) fault <= 1;
                else begin
                    active_generation <= reader_generation;
                    running <= 1; stopped <= 0; stop_requested <= 0;
                    first_slot <= initial_slot; last_slot <= initial_slot;
                    first_sequence <= initial_sequence; last_sequence <= initial_sequence;
                    initial_pending <= 1;
                end
            end
            if (running && !fault && owned) begin
                if (initial_pending && video_ready && !stopping) initial_pending <= 0;
                if (submit_valid && submit_ready) begin
                    if (!valid_submission) fault <= 1;
                    else begin
                        last_slot <= submit_slot;
                        last_sequence <= submit_sequence;
                    end
                end
                if (quiesce) stop_requested <= 1;
                if (stopping && video_stopped) begin
                    running <= 0; stopped <= 1; initial_pending <= 0;
                    // A later Start still needs a retired then newer endpoint.
                    saw_retirement <= 0;
                end
            end
        end
    end
endmodule
