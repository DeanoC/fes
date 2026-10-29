// SPDX-License-Identifier: GPL-2.0-or-later
// Board-local capture: an immutable bank remains owned until scanout swaps
// at an output frame boundary and acknowledges it. Clock loss cannot free it.
module fes_de25_zx81_video (
    input wire clk_sys, reset_n, held, ce_6m5, zx_pixel, hblank, vblank,
    input wire pixel_clk,
    output wire [7:0] red, green, blue,
    output wire de, hsync, vsync
);
    localparam WIDTH = 328, HEIGHT = 270, STRIDE = WIDTH / 8, WORDS = STRIDE * HEIGHT;
    (* ramstyle = "M20K" *) reg [7:0] fb [0:WORDS*2-1];
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] sys_release = 0, pix_release = 0;
    always @(posedge clk_sys or negedge reset_n)
        if (!reset_n) sys_release <= 0; else sys_release <= {sys_release[0], 1'b1};
    always @(posedge pixel_clk or negedge reset_n)
        if (!reset_n) pix_release <= 0; else pix_release <= {pix_release[0], 1'b1};
    wire sys_reset = !sys_release[1], pix_reset = !pix_release[1];
    reg request = 0, acknowledge = 0;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
    reg [1:0] request_sync = 0, acknowledge_sync = 0;
    always @(posedge clk_sys or negedge sys_release[1])
        if (!sys_release[1]) acknowledge_sync <= 0;
        else acknowledge_sync <= {acknowledge_sync[0], acknowledge};
    always @(posedge pixel_clk or negedge pix_release[1])
        if (!pix_release[1]) request_sync <= 0;
        else request_sync <= {request_sync[0], request};

    reg writer = 1, capturing = 0, armed = 0, line_open = 0, bad = 0;
    reg old_hblank = 1, old_vblank = 1;
    reg [9:0] x = 0;
    reg [8:0] rows = 0, published_rows = 0;
    wire pending = request != acknowledge_sync[1];
    wire first_sample = old_hblank && !hblank;
    wire sample = !sys_reset && !held && ce_6m5 && capturing &&
                  (line_open || first_sample) && !pending && !vblank && !hblank &&
                  rows < HEIGHT && x < WIDTH;
    wire [14:0] write_address = (writer ? WORDS : 0) + rows * STRIDE + (x >> 3);
    reg [6:0] lane;
    // Uninitialised RAM is safe: publication covers only completely written rows.
    always @(posedge clk_sys) begin
        if (sample) begin
            lane <= {zx_pixel, lane[6:1]};
            if (x[2:0] == 7) fb[write_address] <= {zx_pixel, lane};
        end
    end

    always @(posedge clk_sys or negedge sys_release[1]) begin
        if (!sys_release[1]) begin
            request <= 0; writer <= 1; capturing <= 0; armed <= 0;
            line_open <= 0; bad <= 0; old_hblank <= 1; old_vblank <= 1;
            x <= 0; rows <= 0; published_rows <= 0;
        end else if (held) begin
            capturing <= 0; armed <= 0; line_open <= 0;
            old_hblank <= 1; old_vblank <= 1; x <= 0; rows <= 0; bad <= 0;
        end else if (ce_6m5) begin
            old_hblank <= hblank;
            old_vblank <= vblank;
            if (vblank) begin
                // VSYNC can start within a line. Drop that partial final line;
                // every preceding completed row is fully written.
                if (!old_vblank && capturing && !bad && rows != 0 && !pending) begin
                    published_rows <= rows;
                    request <= !request;
                end
                capturing <= 0; line_open <= 0; x <= 0; rows <= 0;
                armed <= 1; bad <= 0;
            end else if (old_vblank) begin
                // Start only at a source frame boundary, then a whole line.
                capturing <= armed && !pending;
                writer <= !acknowledge_sync[1];
                line_open <= 0; x <= 0; rows <= 0; armed <= 0;
            end else if (capturing && !pending) begin
                if (first_sample) begin
                    line_open <= 1;
                    x <= rows < HEIGHT ? 10'd1 : 10'd0;
                end else if (!old_hblank && hblank && line_open) begin
                    if (rows < HEIGHT) begin
                        if (x == WIDTH) rows <= rows + 1'b1;
                        else bad <= 1;
                    end
                    x <= 0; line_open <= 0;
                end else if (!hblank && line_open && rows < HEIGHT) begin
                    if (x < WIDTH) x <= x + 1'b1;
                    else bad <= 1;
                end
            end
        end
    end

    reg [10:0] horizontal = 0;
    reg [9:0] vertical = 0;
    reg front = 0, front_valid = 0;
    reg [8:0] front_rows = 0;
    wire boundary = horizontal == 1649 && vertical == 749;
    wire image = horizontal >= 312 && horizontal < 968 && vertical >= 90 && vertical < 630;
    wire [8:0] source_x = (horizontal - 11'd312) >> 1;
    wire [8:0] source_y = (vertical - 10'd90) >> 1;
    wire [14:0] read_address = (front ? WORDS : 0) +
        (image && source_y < front_rows ? source_y * STRIDE + (source_x >> 3) : 0);
    reg [7:0] fb_q;
    reg [2:0] bit_q = 0;
    always @(posedge pixel_clk) fb_q <= fb[read_address];
    // All controls share the RAM's one-edge latency, including the first and
    // last scaled pixels. Missing rows are paper; no stale RAM is selected.
    reg image_q = 0, valid_q = 0, stored_q = 0;
    reg de_q = 0, hs_q = 0, vs_q = 0;
    always @(posedge pixel_clk or negedge pix_release[1]) begin
        if (!pix_release[1]) begin
            horizontal <= 0; vertical <= 0; acknowledge <= 0;
            front <= 0; front_valid <= 0; front_rows <= 0;
            image_q <= 0; valid_q <= 0; stored_q <= 0;
            de_q <= 0; hs_q <= 0; vs_q <= 0; bit_q <= 0;
        end else begin
            horizontal <= horizontal == 1649 ? 11'd0 : horizontal + 1'b1;
            if (horizontal == 1649) vertical <= vertical == 749 ? 10'd0 : vertical + 1'b1;
            if (boundary && request_sync[1] != acknowledge) begin
                front <= request_sync[1]; front_valid <= 1;
                front_rows <= published_rows;
                acknowledge <= request_sync[1];
            end
            de_q <= horizontal < 1280 && vertical < 720;
            hs_q <= horizontal >= 1390 && horizontal < 1430;
            vs_q <= vertical >= 725 && vertical < 730;
            image_q <= image;
            valid_q <= front_valid;
            stored_q <= source_y < front_rows;
            bit_q <= source_x[2:0];
        end
    end
    wire bright = reset_n && image_q && valid_q && (!stored_q || !fb_q[bit_q]);
    assign red = bright ? 8'hff : 8'h00;
    assign green = red;
    assign blue = red;
    assign de = reset_n && de_q;
    assign hsync = reset_n && hs_q;
    assign vsync = reset_n && vs_q;
endmodule
