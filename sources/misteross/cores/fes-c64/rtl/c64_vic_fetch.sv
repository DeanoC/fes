// SPDX-License-Identifier: MIT
// PAL matrix/glyph DMA sequencer. Sprite accesses and pixels are separate.
module c64_vic_fetch (
    input wire clk, reset, first_phase, second_phase, sample,
    input wire [8:0] raster,
    input wire [5:0] cycle,
    input wire [7:0] d011, d018,
    input wire [1:0] bank,
    input wire [7:0] cpu_data,
    input wire [7:0] ram_data,
    input wire [3:0] color_data,
    output wire badline, ba_n, aec_n,
    output reg request, matrix_request,
    output reg [15:0] address,
    output reg [9:0] color_address,
    output reg graphics_valid,
    output reg [7:0] graphics_data, graphics_matrix,
    output reg [3:0] graphics_color,
    output reg [9:0] vc, vcbase,
    output reg [2:0] rc,
    output reg display
);
    reg den_seen;
    reg [2:0] lead_count;
    reg [5:0] vmli, pending_index;
    reg pending_display, pending_owned;
    reg [3:0] pending_cpu_color;
    reg [11:0] matrix_line [0:39];
    wire den_enabled = den_seen || (raster == 9'h030 && d011[4]);
    assign badline = den_enabled && raster >= 9'h030 && raster <= 9'h0f7 &&
        raster[2:0] == d011[2:0];
    assign ba_n = !(badline && cycle >= 6'd12 && cycle <= 6'd54);
    assign aec_n = !(badline && cycle >= 6'd15 && cycle <= 6'd54 && lead_count == 3'd4);
    wire active_display = display;
    wire [7:0] code = vmli < 6'd40 ? matrix_line[vmli][7:0] : 8'd0;
    wire [13:0] normal_g = !active_display ? 14'h3fff :
        d011[5] ? {d018[3], vc, rc} : {d018[3:1], code, rc};
    wire [13:0] glyph_address = d011[6] ? (normal_g & 14'h39ff) : normal_g;
    integer i;
    always @(posedge clk) begin
        graphics_valid <= 1'b0;
        if (reset) begin
            den_seen <= 0; lead_count <= 0; vc <= 0; vcbase <= 0;
            rc <= 7; vmli <= 0; display <= 0; request <= 0;
            matrix_request <= 0; address <= 0; color_address <= 0;
            pending_index <= 0; pending_display <= 0; pending_owned <= 0; pending_cpu_color <= 0;
            graphics_data <= 0; graphics_matrix <= 0; graphics_color <= 0;
            for (i=0; i<40; i=i+1) matrix_line[i] <= 0;
        end else begin
            if (first_phase) begin
                request <= 0;
                if (raster == 0) begin den_seen <= 0; vcbase <= 0; end
                else if (raster == 9'h030 && d011[4]) den_seen <= 1;
                if (ba_n) lead_count <= 0;
                else if (lead_count < 4) lead_count <= lead_count + 1'b1;
                if (badline) display <= 1;
                if (cycle == 14) begin
                    vc <= vcbase; vmli <= 0;
                    if (badline) rc <= 0;
                end
                if (cycle >= 16 && cycle <= 55) begin
                    request <= 1; matrix_request <= 0;
                    address <= {bank, glyph_address};
                    pending_index <= vmli; pending_display <= active_display;
                    if (active_display) begin vc <= vc + 1'b1; vmli <= vmli + 1'b1; end
                end
                if (cycle == 58) begin
                    if (rc == 7) begin display <= badline; vcbase <= vc; end
                    if ((display && rc != 7) || badline) rc <= rc + 1'b1;
                end
            end
            if (second_phase) begin
                request <= 0;
                if (badline && cycle >= 15 && cycle <= 54) begin
                    request <= 1; matrix_request <= 1;
                    address <= {bank, d018[7:4], vc}; color_address <= vc;
                    pending_index <= vmli; pending_owned <= !aec_n; pending_cpu_color <= cpu_data[3:0];
                end
            end
            if (sample && request) begin
                if (matrix_request) begin
                    if (pending_index < 40)
                        matrix_line[pending_index] <= {pending_owned ? color_data : pending_cpu_color, pending_owned ? ram_data : 8'hff};
                end else begin
                    graphics_valid <= 1;
                    graphics_data <= ram_data;
                    graphics_matrix <= pending_display && pending_index < 40 ? matrix_line[pending_index][7:0] : 8'd0;
                    graphics_color <= pending_display && pending_index < 40 ? matrix_line[pending_index][11:8] : 4'd0;
                end
                request <= 0;
            end
        end
    end
endmodule
