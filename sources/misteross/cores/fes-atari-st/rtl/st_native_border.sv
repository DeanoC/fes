// SPDX-License-Identifier: GPL-3.0-or-later
// Lossless palette-zero runs for the visible ST border. The surrounding native
// capture owns these same three banks; it must reject overflowed captures and
// pin the selected bank until output SOF. No independent publication protocol.
module st_native_border #(parameter integer EVENT_BITS = 12) (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel,
    input wire capture_start, capture_active, capture_pal, capture_tick, capture_display,
    input wire [1:0] capture_bank,
    input wire [8:0] native_line, native_cycle,
    input wire [8:0] palette_zero,
    output reg capture_overflow,
    output reg [2:0] raster_banks,
    // Row is established during output blanking. Line start loads its run
    // descriptor; SOF always starts row zero at event zero in the new bank.
    input wire [1:0] output_bank,
    input wire output_sof, output_line_start, output_ce,
    input wire [8:0] output_row, output_x,
    output wire [8:0] output_rgb
);
    localparam integer EVENTS = 1 << EVENT_BITS;
    reg [17:0] events0 [0:EVENTS-1];
    reg [17:0] events1 [0:EVENTS-1];
    reg [17:0] events2 [0:EVENTS-1];
    // Each visible row has an initial event at x=0, then only colour changes.
    // Rows repeat independently at HDMI, so a descriptor restarts every row.
    reg [EVENT_BITS+8:0] rows0 [0:275];
    reg [EVENT_BITS+8:0] rows1 [0:275];
    reg [EVENT_BITS+8:0] rows2 [0:275];
    reg [EVENT_BITS:0] event_count;
    reg [8:0] row_count, last_colour, first_colour;
    reg [EVENT_BITS-1:0] row_offset;
    wire [8:0] visible_top = capture_pal ? 9'd34 : 9'd5;
    wire [8:0] row = native_line - visible_top;
    wire visible = native_line >= visible_top && row < (capture_pal ? 9'd276 : 9'd255) &&
                   native_cycle >= (capture_pal ? 9'd8 : 9'd4) &&
                   native_cycle < (capture_pal ? 9'd424 : 9'd420);
    wire [8:0] x = native_cycle - (capture_pal ? 9'd8 : 9'd4);
    wire new_run = x == 9'd0 || palette_zero != last_colour;
    wire append = capture_active && capture_tick && visible && new_run && !capture_overflow;
    wire [8:0] next_row_count = x == 9'd0 ? 9'd1 : row_count + 9'd1;
    wire [EVENT_BITS-1:0] next_row_offset = x == 9'd0 ? event_count[EVENT_BITS-1:0] : row_offset;
    always @(posedge clk_sys) begin
        if (reset_sys || capture_start) begin
            if (reset_sys) raster_banks <= 3'd0;
            else raster_banks[capture_bank] <= 1'b0;
            first_colour <= 0;
            event_count <= 0; row_count <= 0; row_offset <= 0;
            last_colour <= 0; capture_overflow <= 1'b0;
        end else begin
            if (capture_active && capture_tick && visible && !capture_display &&
                event_count != 0 && palette_zero != first_colour) raster_banks[capture_bank] <= 1'b1;
            if (append) begin
                if (event_count == 0) first_colour <= palette_zero;
                if (event_count == (EVENT_BITS+1)'(EVENTS)) capture_overflow <= 1'b1;
                else begin
                    event_count <= event_count + 1'b1;
                    row_count <= next_row_count; row_offset <= next_row_offset;
                    last_colour <= palette_zero;
                    case (capture_bank)
                        2'd0: begin
                            events0[event_count[EVENT_BITS-1:0]] <= {x, palette_zero};
                            rows0[row] <= {next_row_count, next_row_offset};
                        end
                        2'd1: begin
                            events1[event_count[EVENT_BITS-1:0]] <= {x, palette_zero};
                            rows1[row] <= {next_row_count, next_row_offset};
                        end
                        2'd2: begin
                            events2[event_count[EVENT_BITS-1:0]] <= {x, palette_zero};
                            rows2[row] <= {next_row_count, next_row_offset};
                        end
                        default: capture_overflow <= 1'b1;
                    endcase
                end
            end
        end
    end

    reg [EVENT_BITS+8:0] desc0, desc1, desc2;
    reg [17:0] head0, head1, head2;
    wire [EVENT_BITS+8:0] descriptor = output_bank == 2'd0 ? desc0 : output_bank == 2'd1 ? desc1 : desc2;
    wire [17:0] head = output_bank == 2'd0 ? head0 : output_bank == 2'd1 ? head1 : head2;
    reg [EVENT_BITS-1:0] pointer;
    reg [8:0] remaining, colour;
    wire consume = output_ce && remaining != 9'd0 && head[17:9] <= output_x;
    wire [EVENT_BITS-1:0] read_pointer = output_sof ? {EVENT_BITS{1'b0}} :
        output_line_start ? descriptor[EVENT_BITS-1:0] : consume && remaining > 9'd1 ? pointer + 1'b1 : pointer;
    assign output_rgb = consume ? head[8:0] : colour;
    // Unconditional clocked ports infer dual-clock M10Ks. The parent owns
    // cross-domain bank selection and the descriptor/event immutability.
    always @(posedge clk_pixel) begin
        desc0 <= rows0[output_row]; desc1 <= rows1[output_row]; desc2 <= rows2[output_row];
        head0 <= events0[read_pointer]; head1 <= events1[read_pointer]; head2 <= events2[read_pointer];
        if (reset_pixel) begin
            pointer <= 0; remaining <= 0; colour <= 0;
        end else if (output_sof || output_line_start) begin
            pointer <= read_pointer;
            remaining <= descriptor[EVENT_BITS+8:EVENT_BITS];
            colour <= 0;
        end else if (consume) begin
            colour <= head[8:0]; remaining <= remaining - 1'b1;
            if (remaining > 9'd1) pointer <= pointer + 1'b1;
        end
    end
endmodule
