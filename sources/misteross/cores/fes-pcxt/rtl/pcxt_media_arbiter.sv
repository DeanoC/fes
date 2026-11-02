// SPDX-License-Identifier: GPL-2.0-or-later
// Serialize the host loader and committed guest writes, and share disk reads
// between the FDC and a frozen-media snapshot. Each accepted operation keeps
// its owner until the SDRAM reply; the low gap rearms the memory port.
module pcxt_media_arbiter (
    input wire clk, cold_reset, machine_reset,
    input wire host_valid, output wire host_ready, host_done,
    input wire [18:0] host_address, input wire [7:0] host_data,
    input wire guest_valid, output wire guest_ready, guest_done,
    input wire [18:0] guest_address, input wire [7:0] guest_data,
    output wire storage_valid, input wire storage_ready, storage_done,
    output wire [18:0] storage_address, output wire [7:0] storage_data,
    input wire fdc_valid, input wire [18:0] fdc_address,
    output wire fdc_ready, output wire [7:0] fdc_data,
    input wire snapshot_valid, input wire [18:0] snapshot_address,
    output wire snapshot_ready, output wire [7:0] snapshot_data,
    output wire read_valid, read_reset_safe, output wire [18:0] read_address,
    input wire read_ready, input wire [7:0] read_data
);
    localparam [1:0] W_IDLE=0, W_WAIT=1, W_GAP=2;
    reg [1:0] write_state = W_IDLE;
    reg write_guest = 0;
    wire select_guest = guest_valid;
    assign storage_valid = write_state == W_IDLE && (guest_valid || host_valid);
    assign storage_address = select_guest ? guest_address : host_address;
    assign storage_data = select_guest ? guest_data : host_data;
    assign guest_ready = write_state == W_IDLE && select_guest && storage_ready;
    assign host_ready = write_state == W_IDLE && !select_guest && storage_ready;
    assign guest_done = write_state == W_WAIT && write_guest && storage_done;
    assign host_done = write_state == W_WAIT && !write_guest && storage_done;
    always @(posedge clk) begin
        if (cold_reset) begin write_state <= W_IDLE; write_guest <= 0; end
        else case (write_state)
            W_IDLE: if (storage_valid && storage_ready) begin
                write_guest <= select_guest;
                write_state <= W_WAIT;
            end
            W_WAIT: if (storage_done) write_state <= W_GAP;
            W_GAP: write_state <= W_IDLE;
            default: write_state <= W_IDLE;
        endcase
    end

    localparam [1:0] R_IDLE=0, R_WAIT=1, R_GAP=2;
    reg [1:0] read_state = R_IDLE;
    reg read_snapshot = 0, prefer_snapshot = 0;
    reg [18:0] held_read_address = 0;
    wire fdc_request = fdc_valid && !machine_reset;
    wire choose_snapshot = snapshot_valid && (!fdc_request || prefer_snapshot);
    assign read_valid = read_state == R_WAIT && (read_snapshot || !machine_reset);
    assign read_reset_safe = read_snapshot && read_valid;
    assign read_address = held_read_address;
    assign fdc_ready = read_state == R_WAIT && !read_snapshot && !machine_reset && read_ready;
    assign snapshot_ready = read_state == R_WAIT && read_snapshot && read_ready;
    assign fdc_data = read_data;
    assign snapshot_data = read_data;
    always @(posedge clk) begin
        if (cold_reset) begin
            read_state <= R_IDLE; read_snapshot <= 0;
            prefer_snapshot <= 0; held_read_address <= 0;
        end else if (machine_reset && read_state == R_WAIT && !read_snapshot) begin
            read_state <= R_IDLE;
        end else case (read_state)
            R_IDLE: if (fdc_request || snapshot_valid) begin
                read_snapshot <= choose_snapshot;
                held_read_address <= choose_snapshot ? snapshot_address : fdc_address;
                read_state <= R_WAIT;
            end
            R_WAIT: if (read_ready) begin
                prefer_snapshot <= !read_snapshot;
                read_state <= R_GAP;
            end
            R_GAP: read_state <= R_IDLE;
            default: read_state <= R_IDLE;
        endcase
    end
endmodule
