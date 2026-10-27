// SPDX-License-Identifier: GPL-3.0-or-later
// Lossless palette-zero runs for the visible ST border. The surrounding native
// capture owns these same three banks; it must reject overflowed captures and
// pin the selected bank until output SOF. No independent publication protocol.
module st_native_border #(parameter integer EVENT_BITS = 13) (
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
    // Parent front bank is registered. It equals output_bank away from SOF;
    // SOF loads the new descriptor and overrides ordinary consumption.
    input wire [1:0] output_consume_bank,
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
                    if (capture_bank == 2'd3) capture_overflow <= 1'b1;
                end
            end
        end
    end
    // Register each run write and commit it to its bank's RAMs one edge later,
    // so reset, capture start and the run decision end at these registers
    // instead of at the M10K enables. Output reads a bank only after the
    // parent publishes it at frame end, long after its last run is written.
    reg write_valid;
    reg [1:0] write_bank;
    reg [EVENT_BITS-1:0] write_event;
    reg [17:0] write_run;
    reg [8:0] write_row;
    reg [EVENT_BITS+8:0] write_descriptor;
    always @(posedge clk_sys) begin
        write_valid <= !reset_sys && !capture_start && append && event_count != (EVENT_BITS+1)'(EVENTS) &&
                       capture_bank != 2'd3;
        write_bank <= capture_bank;
        write_event <= event_count[EVENT_BITS-1:0];
        write_run <= {x, palette_zero};
        write_row <= row;
        write_descriptor <= {next_row_count, next_row_offset};
    end
    always @(posedge clk_sys) begin
        if (write_valid) begin
            case (write_bank)
                2'd0: begin events0[write_event] <= write_run; rows0[write_row] <= write_descriptor; end
                2'd1: begin events1[write_event] <= write_run; rows1[write_row] <= write_descriptor; end
                default: begin events2[write_event] <= write_run; rows2[write_row] <= write_descriptor; end
            endcase
        end
    end

    // The parent sets output_row in blanking, several edges before line start
    // and SOF. Each descriptor RAM's read data takes one more edge into a
    // fabric register, so no descriptor RAM output feeds an event address.
    reg [EVENT_BITS+8:0] row_read0, row_read1, row_read2;
    reg [EVENT_BITS+8:0] desc0, desc1, desc2;
    reg [17:0] head0, head1, head2;
    wire [8:0] descriptor_count = output_bank == 2'd0 ? desc0[EVENT_BITS+8:EVENT_BITS] :
        output_bank == 2'd1 ? desc1[EVENT_BITS+8:EVENT_BITS] : desc2[EVENT_BITS+8:EVENT_BITS];
    // The consume bank changes only at SOF, which also clears held_valid, so
    // its prefetched head is selected on the way into the held event.
    wire [17:0] consume_bank_head = output_consume_bank == 2'd0 ? head0 :
        output_consume_bank == 2'd1 ? head1 : head2;
    reg [17:0] held;
    reg held_valid;
    reg [EVENT_BITS-1:0] next_pointer;
    // The published display bank is immutable until SOF. Hold its current
    // event through pauses while the RAM ports prefetch the successor. Only
    // the held event is compared, so no event RAM output reaches the consume
    // decision. held_valid is clear for the edge after SOF, line start or a
    // consume, while the successor loads. The parent starts rows in blanking
    // and holds each output_x for two output_ce edges; a row's event x values
    // strictly increase, so no event is due on the edge after a consume.
    // No head-X comparison or consume decision feeds the RAM read address.
    reg [8:0] remaining, colour;
    wire consume_current = output_ce && held_valid && remaining != 9'd0 && held[17:9] <= output_x;
    // Each RAM uses its own row descriptor on line start. The selected bank
    // therefore reaches its event address without a cross-bank descriptor mux;
    // the other banks' prefetched heads remain speculative.
    wire [EVENT_BITS-1:0] read_pointer0 = output_sof ? {EVENT_BITS{1'b0}} :
        output_line_start ? desc0[EVENT_BITS-1:0] : next_pointer;
    wire [EVENT_BITS-1:0] read_pointer1 = output_sof ? {EVENT_BITS{1'b0}} :
        output_line_start ? desc1[EVENT_BITS-1:0] : next_pointer;
    wire [EVENT_BITS-1:0] read_pointer2 = output_sof ? {EVENT_BITS{1'b0}} :
        output_line_start ? desc2[EVENT_BITS-1:0] : next_pointer;
    // Publication can select a bank on this same SOF edge. Increment each
    // descriptor independently so that late bank selection does not precede
    // an EVENT_BITS-wide carry chain. SOF retains its fixed event-zero origin.
    wire [EVENT_BITS-1:0] successor0 = desc0[EVENT_BITS-1:0] + 1'b1;
    wire [EVENT_BITS-1:0] successor1 = desc1[EVENT_BITS-1:0] + 1'b1;
    wire [EVENT_BITS-1:0] successor2 = desc2[EVENT_BITS-1:0] + 1'b1;
    wire [EVENT_BITS-1:0] line_successor = output_sof ? {{(EVENT_BITS-1){1'b0}}, 1'b1} :
        output_bank == 2'd0 ? successor0 : output_bank == 2'd1 ? successor1 : successor2;
    // SOF's new bank affects descriptor loading, not visible consumption:
    // the fixed raster issues no border sample at SOF. Away from SOF the
    // registered front bank is the same selected bank. Keep publication
    // arbitration out of both the consume decision and RGB forwarding.
    assign output_rgb = consume_current ? held[8:0] : colour;
    // Unconditional clocked ports infer dual-clock M10Ks. The parent owns
    // cross-domain bank selection and the descriptor/event immutability.
    always @(posedge clk_pixel) begin
        row_read0 <= rows0[output_row]; row_read1 <= rows1[output_row]; row_read2 <= rows2[output_row];
        desc0 <= row_read0; desc1 <= row_read1; desc2 <= row_read2;
        head0 <= events0[read_pointer0]; head1 <= events1[read_pointer1]; head2 <= events2[read_pointer2];
        if (reset_pixel) begin
            next_pointer <= 1; remaining <= 0; colour <= 0;
            held_valid <= 1'b0;
        end else if (output_sof || output_line_start) begin
            held_valid <= 1'b0;
            next_pointer <= line_successor;
            remaining <= descriptor_count;
            colour <= 0;
        end else if (consume_current) begin
            held_valid <= 1'b0;
            colour <= held[8:0]; remaining <= remaining - 1'b1;
            if (remaining > 9'd1) next_pointer <= next_pointer + 1'b1;
        end else if (!held_valid) begin
            held_valid <= 1'b1;
        end
        // The held event is read only while held_valid, so load it whenever
        // it is clear. Neither the consume decision nor reset enables it.
        if (!held_valid) held <= consume_bank_head;
    end
endmodule
