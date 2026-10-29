// SPDX-License-Identifier: MIT
// pc386 development link to the HPS through the DDR window
// (docs/pc386-machine-design.md, phase M5), on DDR port 2's single-beat
// master. Until the fes.computer 1.1 contracts land (M0), the HPS stages the
// ATA image in the window itself and hands over the machine's straps, the
// disk geometry and the wall clock in a configuration block; the machine
// stays in reset until it arrives. The link also serves the ATA image port
// and reports what the machine does: a status block written every
// STATUS_CLOCKS and a ring of the debug-port bytes.
//
// Window layout (byte offsets from WINDOW_BASE; 64-bit words):
//
//   0x01000000  configuration, written by the HPS:
//                 0  CONFIG_MAGIC [31:0], epoch [63:32]
//                 1  base_kib [15:0], extended_kib [31:16], above16m_64k [47:32]
//                 2  sb_base_high [7:0], sb_irq [15:8], sb_dma [23:16],
//                    disk_present [32], queue16 [33], wall clock valid [34]
//                 3  disk_sectors [31:0], disk_cylinders [47:32],
//                    disk_heads [52:48], disk_spt [61:56]
//                 4  wall clock: year [15:0], month [23:16], day [31:24],
//                    hour [39:32], minute [47:40], second [55:48], weekday [63:56]
//   0x01000040  status, written here:
//                 0  STATUS_MAGIC [31:0], epoch [63:32]
//                 1  running [0], debug bytes dropped [31:16], debug bytes [63:32]
//                 2-7  observe, the shell's words
//   0x01001000  debug ring: debug byte n at offset n mod 4096
//   0x08000000  ATA image, 64-bit words
//
// Out of reset the link clears configuration word 0, so no block left by an
// earlier run is taken, then writes status word 0 with its epoch: the clock
// count since FPGA configuration. The HPS waits for STATUS_MAGIC with a new
// epoch there, writes the image and words 1-4, then word 0 with the same
// epoch. The link reads word 0 every POLL_CLOCKS and, once it matches,
// reads words 1-4, latches them and releases the machine. cpu_hold stays
// high until the RTC has taken the wall clock, which it can only once its
// CMOS setup is done.
module pc386_host_link #(
    parameter [31:0] WINDOW_BASE = 32'h30000000,
    parameter [31:0] CONFIG_MAGIC = 32'h43363833,  // "386C"
    parameter [31:0] STATUS_MAGIC = 32'h53363833,  // "386S"
    parameter integer POLL_CLOCKS = 4096,
    parameter integer STATUS_CLOCKS = 65536
) (
    input wire clk, reset,  // reset: port 2's execution reset
    output reg machine_hold,
    output wire cpu_hold,
    // Straps, disk geometry and wall clock from the configuration
    output reg [15:0] base_kib, extended_kib, above16m_64k,
    output reg [7:0] sb_base_high, sb_irq, sb_dma,
    output reg disk_present, queue16,
    output reg [31:0] disk_sectors,
    output reg [15:0] disk_cylinders,
    output reg [4:0] disk_heads,
    output reg [5:0] disk_spt,
    input wire cmos_busy,
    output reg wall_valid,
    output reg [15:0] wall_year,
    output reg [7:0] wall_month, wall_day, wall_hour, wall_minute, wall_second, wall_weekday,
    // ATA image port
    input wire disk_valid, disk_write,
    input wire [23:0] disk_address,
    input wire [63:0] disk_wdata,
    output reg disk_ready, disk_rvalid,
    output reg [63:0] disk_rdata,
    // Debug-port bytes, and status words 2-7
    input wire debug_valid,
    input wire [7:0] debug_byte,
    input wire [6*64-1:0] observe,
    // Port 2's single-beat master
    output reg [28:0] address,
    output reg read, write,
    output reg [63:0] writedata,
    output reg [7:0] byteenable,
    input wire waitrequest,
    input wire [63:0] readdata,
    input wire readdatavalid
);
    localparam [28:0] BASE = WINDOW_BASE[31:3];
    localparam [28:0] CONFIG = BASE + 29'h0200000, STATUS = BASE + 29'h0200008;
    localparam [28:0] RING = BASE + 29'h0200200, IMAGE = BASE + 29'h1000000;
    localparam [3:0] S_CLEAR = 4'd0, S_ANNOUNCE = 4'd1, S_WAIT = 4'd2, S_POLL = 4'd3, S_CONFIG = 4'd4,
        S_WALL = 4'd5, S_RUN = 4'd6, S_DISK = 4'd7, S_DEBUG = 4'd8, S_STATUS = 4'd9;

    // Free-running from configuration, so each launch draws a new epoch.
    reg [31:0] clock = 32'd0;
    always @(posedge clk) clock <= clock + 32'd1;

    // Debug bytes wait here for the port.
    wire debug_full, debug_empty;
    wire [7:0] debug_next;
    wire [4:0] debug_stored;
    reg debug_pop;
    pc386_fifo #(.WIDTH(8), .DEPTH(16)) debug_fifo (
        .clk(clk), .reset(reset || machine_hold), .push(debug_valid && !debug_full), .din(debug_byte),
        .full(debug_full), .pop(debug_pop), .dout(debug_next), .empty(debug_empty), .count(debug_stored)
    );

    reg [3:0] state;
    reg [31:0] epoch, debug_count, countdown;
    reg [15:0] debug_drops;
    reg [2:0] word;
    reg waiting;  // a read's data is due
    reg running, wall_pending;
    wire [8*64-1:0] status = {observe, debug_count, debug_drops, 15'd0, running, epoch, STATUS_MAGIC};
    wire [2:0] next_word = word + 3'd1;
    wire taken = (read || write) && !waitrequest;
    assign cpu_hold = machine_hold || wall_pending || wall_valid;

    always @(posedge clk) begin
        disk_ready <= 0;
        disk_rvalid <= 0;
        debug_pop <= 0;
        wall_valid <= 0;
        if (reset) begin
            state <= S_CLEAR; machine_hold <= 1; running <= 0; wall_pending <= 0;
            read <= 0; write <= 0; waiting <= 0; address <= 0; writedata <= 0; byteenable <= 0;
            epoch <= clock; debug_count <= 0; debug_drops <= 0; word <= 0; countdown <= 0;
            base_kib <= 0; extended_kib <= 0; above16m_64k <= 0; sb_base_high <= 0; sb_irq <= 0; sb_dma <= 0;
            disk_present <= 0; queue16 <= 0; disk_sectors <= 0; disk_cylinders <= 0; disk_heads <= 0;
            disk_spt <= 0; disk_rdata <= 0;
            wall_year <= 0; wall_month <= 0; wall_day <= 0; wall_hour <= 0; wall_minute <= 0;
            wall_second <= 0; wall_weekday <= 0;
        end else begin
            if (debug_valid && debug_full) debug_drops <= debug_drops + 16'd1;
            if (running) countdown <= countdown == 32'd0 ? 32'd0 : countdown - 32'd1;
            // The machine takes the wall clock once its CMOS setup is done.
            if (wall_pending && !machine_hold && !cmos_busy) begin wall_valid <= 1; wall_pending <= 0; end
            if (taken) begin
                read <= 0; write <= 0;
                waiting <= read;
            end
            case (state)
                S_CLEAR: if (!write && !taken) begin
                    address <= CONFIG; write <= 1; writedata <= 64'd0; byteenable <= 8'hff;
                    state <= S_ANNOUNCE;
                end
                S_ANNOUNCE: if (!write && !taken) begin
                    address <= STATUS; write <= 1; writedata <= {epoch, STATUS_MAGIC};
                    countdown <= POLL_CLOCKS; state <= S_WAIT;
                end
                S_WAIT: begin
                    if (countdown != 32'd0) countdown <= countdown - 32'd1;
                    else if (!write) begin
                        address <= CONFIG; read <= 1; state <= S_POLL;
                    end
                end
                S_POLL: if (waiting && readdatavalid) begin
                    waiting <= 0;
                    if (readdata == {epoch, CONFIG_MAGIC}) begin
                        address <= CONFIG + 29'd1; read <= 1; word <= 3'd1; state <= S_CONFIG;
                    end else begin
                        countdown <= POLL_CLOCKS; state <= S_WAIT;
                    end
                end
                S_CONFIG: if (waiting && readdatavalid) begin
                    waiting <= 0;
                    case (word)
                        3'd1: {above16m_64k, extended_kib, base_kib} <= readdata[47:0];
                        3'd2: begin
                            {sb_dma, sb_irq, sb_base_high} <= readdata[23:0];
                            disk_present <= readdata[32]; queue16 <= readdata[33]; wall_pending <= readdata[34];
                        end
                        3'd3: begin
                            disk_sectors <= readdata[31:0]; disk_cylinders <= readdata[47:32];
                            disk_heads <= readdata[52:48]; disk_spt <= readdata[61:56];
                        end
                        default: begin
                            {wall_weekday, wall_second, wall_minute, wall_hour, wall_day, wall_month, wall_year}
                                <= readdata;
                        end
                    endcase
                    if (word == 3'd4) state <= S_WALL;
                    else begin address <= address + 29'd1; read <= 1; word <= word + 3'd1; end
                end
                S_WALL: begin
                    // The straps are in place a clock before the machine leaves reset.
                    machine_hold <= 0; running <= 1; countdown <= 0; state <= S_RUN;
                end
                S_RUN: begin
                    // The ATA image first, then debug bytes, then the status block.
                    if (disk_valid && !disk_ready) begin
                        address <= IMAGE + {5'd0, disk_address}; read <= !disk_write; write <= disk_write;
                        writedata <= disk_wdata; byteenable <= 8'hff; state <= S_DISK;
                    end else if (!debug_empty) begin
                        address <= RING + {20'd0, debug_count[11:3]}; write <= 1;
                        writedata <= {8{debug_next}}; byteenable <= 8'd1 << debug_count[2:0];
                        debug_pop <= 1; debug_count <= debug_count + 32'd1; state <= S_DEBUG;
                    end else if (countdown == 32'd0) begin
                        word <= 3'd0; address <= STATUS; write <= 1; writedata <= status[63:0]; byteenable <= 8'hff;
                        countdown <= STATUS_CLOCKS; state <= S_STATUS;
                    end
                end
                S_DISK: if (taken && write) begin
                    disk_ready <= 1; state <= S_RUN;
                end else if (taken) disk_ready <= 1;
                else if (waiting && readdatavalid) begin
                    waiting <= 0; disk_rvalid <= 1; disk_rdata <= readdata; state <= S_RUN;
                end
                S_DEBUG: if (taken) state <= S_RUN;
                S_STATUS: if (taken) begin
                    if (word == 3'd7) state <= S_RUN;
                    else begin
                        word <= next_word; address <= address + 29'd1; write <= 1;
                        writedata <= status[next_word * 64 +: 64];
                    end
                end
                default: state <= S_CLEAR;
            endcase
        end
    end
    wire unused = ^debug_stored;
endmodule
