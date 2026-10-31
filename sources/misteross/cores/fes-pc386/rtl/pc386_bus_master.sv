// SPDX-License-Identifier: MIT
// pc386 M4 bus master (docs/pc386-machine-design.md, phase M4): the CPU's
// stand-in in the diagnostic shell. It runs commands that the HPS writes into
// a ring in the DDR window and issues each as the CPU would: OUT, IN and INTA
// on the machine's I/O port, reads and writes on its A0000h-BFFFFh MMIO port.
// It also answers the 8237's memory cycles from guest RAM and the ATA image
// port from the image, both in the window. All of it uses DDR port 0, which
// is the CPU's alone in the full machine.
//
// Window layout (byte offsets from WINDOW_BASE; 16-byte beats):
//
//   0x01000000  doorbell, written by the HPS: tail (commands submitted), epoch
//   0x01000010  status, written here: head (commands done), MAGIC, epoch, clock
//   0x01010000  4096 commands: {count/port[31:16], flags[15:8], op[7:0]},
//               address, data, epoch + sequence number
//   0x01020000  4096 results: data, clocks taken, status, epoch + sequence number
//   0x04000000  guest RAM at the 8237's 24-bit address
//   0x08000000  ATA image, 64-bit words
//
// After reset the master clears the doorbell to {tail 0, its epoch}, so no
// tail left by an earlier run is honoured, then writes the status beat with
// that epoch (the clock count since configuration). A host attaches when it
// sees MAGIC there, and writes the doorbell with the same epoch.
// Every command and result carries epoch + its sequence number, so a stale
// beat from an earlier run or an earlier lap of the ring is never executed:
// the master reads the command again until the sequence number matches.
//
//   op  name       flags        count      address      data
//    0  NOP
//    1  OUT        size         port                    value
//    2  IN         size         port                                -> value
//    3  MMIO_WR    byte enables            byte address value
//    4  MMIO_RD                            byte address             -> value
//    5  FILL                    dwords     byte address value
//    6  COPY_IN                 dwords     byte address window offset (DDR to MMIO)
//    7  COPY_OUT                dwords     byte address window offset (MMIO to DDR)
//    8  INTA                                                        -> vector
//    9  WAIT_INTR                                       clocks      -> INTR seen
//   10  DELAY                                           clocks
//   11  POLL_IN    size         port       mask         value       -> last value
//   12  CONFIG                             index        value
//   13  READ                               index                    -> value
//   14  KEY                                             scan code
//
// size: 0 byte, 1 word, 2 dword. MMIO byte addresses are dword-aligned and,
// with the dwords that follow, inside A0000h-BFFFFh; window offsets are
// beat-aligned and, with the dwords that follow, inside the window. Result
// status: 0 done; 1 timed out: POLL_IN never matched, or the machine did not
// answer within TIMEOUT_CLOCKS (as while it is held in reset), which bounds
// every command but WAIT_INTR and DELAY; 2 bad op, index or size; 3 an MMIO
// address or window offset out of its range. Commands with status 2 or 3 do
// nothing. CONFIG 0 releases (0) or holds (1) the machine in reset; the master
// starts with it held so that the straps below are set first. CONFIG 1-11 set
// base_kib, extended_kib, above16m_64k, sb_base_high, sb_irq, sb_dma,
// disk_present, disk_sectors, disk_cylinders, disk_heads and disk_spt; 12 sets
// dirty_index. READ 0-15 return the observe words, READ 16 the clock count.
module pc386_bus_master #(
    parameter [31:0] WINDOW_BASE = 32'h30000000,
    parameter [31:0] MAGIC = 32'h33384d34,              // "4M83"
    parameter integer POLL_CLOCKS = 255,
    parameter [31:0] TIMEOUT_CLOCKS = 32'd10000000
) (
    input wire clk, reset,
    output reg machine_hold,
    // CPU I/O and interrupt acknowledge
    output reg io_valid, io_write, inta_valid,
    output reg [15:0] io_port,
    output reg [1:0] io_size,
    output reg [31:0] io_wdata,
    input wire io_busy, io_done, inta_done,
    input wire [31:0] io_rdata,
    input wire [7:0] inta_vector,
    input wire intr,
    // CPU MMIO for A0000h-BFFFFh
    output reg mem_valid, mem_write,
    output reg [16:2] mem_address,
    output reg [3:0] mem_byteenable,
    output reg [31:0] mem_wdata,
    input wire mem_ready, mem_rvalid,
    input wire [31:0] mem_rdata,
    // The 8237's memory cycles
    input wire dma_valid, dma_write,
    input wire [23:0] dma_address,
    input wire [7:0] dma_wdata,
    output reg dma_done,
    output reg [7:0] dma_rdata,
    // ATA image port
    input wire disk_valid, disk_write,
    input wire [23:0] disk_address,
    input wire [63:0] disk_wdata,
    output reg disk_ready, disk_rvalid,
    output reg [63:0] disk_rdata,
    // Keyboard
    output reg key_valid,
    output reg [7:0] key_code,
    input wire key_ready,
    // Straps
    output reg [15:0] base_kib, extended_kib, above16m_64k,
    output reg [7:0] sb_base_high, sb_irq, sb_dma,
    output reg disk_present,
    output reg [31:0] disk_sectors,
    output reg [15:0] disk_cylinders,
    output reg [4:0] disk_heads,
    output reg [5:0] disk_spt,
    output reg [6:0] dirty_index,
    input wire [16*32-1:0] observe,
    // DDR port 0
    output reg [27:0] p0_address,
    output wire [7:0] p0_burstcount,
    output reg p0_read, p0_write,
    output reg [127:0] p0_writedata,
    output reg [15:0] p0_byteenable,
    input wire p0_waitrequest,
    input wire [127:0] p0_readdata,
    input wire p0_readdatavalid
);
    localparam [27:0] BASE = WINDOW_BASE[31:4];
    localparam [27:0] DOORBELL = BASE + 28'h0100000, STATUS = BASE + 28'h0100001;
    localparam [27:0] COMMANDS = BASE + 28'h0101000, RESULTS = BASE + 28'h0102000;
    localparam [27:0] GUEST = BASE + 28'h0400000, IMAGE = BASE + 28'h0800000;
    localparam [7:0] OP_NOP = 8'd0, OP_OUT = 8'd1, OP_IN = 8'd2, OP_MMIO_WR = 8'd3, OP_MMIO_RD = 8'd4,
        OP_FILL = 8'd5, OP_COPY_IN = 8'd6, OP_COPY_OUT = 8'd7, OP_INTA = 8'd8, OP_WAIT_INTR = 8'd9,
        OP_DELAY = 8'd10, OP_POLL_IN = 8'd11, OP_CONFIG = 8'd12, OP_READ = 8'd13, OP_KEY = 8'd14;
    assign p0_burstcount = 8'd1;

    // Free-running from configuration, so each launch draws a new epoch.
    reg [31:0] clock = 32'd0;
    always @(posedge clk) clock <= clock + 32'd1;

    // ---- Port 0: one transaction at a time; DMA, then disk, then the engine.
    localparam [1:0] NONE = 2'd0, DMA = 2'd1, DISK = 2'd2, ENGINE = 2'd3;
    reg [1:0] owner;
    reg waiting;                                   // a read's data is due
    reg engine_request, engine_write;
    reg [27:0] engine_address;
    reg [127:0] engine_wdata;
    reg [15:0] engine_be;
    reg engine_done;
    reg [127:0] read_beat;
    reg dma_started, disk_started, disk_half;
    wire [27:0] dma_word = GUEST + {8'd0, dma_address[23:4]};
    wire [27:0] disk_word = IMAGE + {5'd0, disk_address[23:1]};
    always @(posedge clk) begin
        engine_done <= 0;
        dma_done <= 0;
        disk_ready <= 0;
        disk_rvalid <= 0;
        if (reset) begin
            owner <= NONE; waiting <= 0; p0_read <= 0; p0_write <= 0;
            p0_address <= 0; p0_writedata <= 0; p0_byteenable <= 0;
            dma_started <= 0; disk_started <= 0; disk_half <= 0;
            read_beat <= 0; dma_rdata <= 0; disk_rdata <= 0;
        end else if (owner == NONE) begin
            if (dma_valid && !dma_started && !dma_done) begin
                owner <= DMA; dma_started <= 1;
                p0_address <= dma_word; p0_read <= !dma_write; p0_write <= dma_write;
                p0_writedata <= {16{dma_wdata}}; p0_byteenable <= 16'd1 << dma_address[3:0];
            end else if (disk_valid && !disk_started && !disk_ready) begin
                owner <= DISK; disk_started <= 1; disk_half <= disk_address[0];
                p0_address <= disk_word; p0_read <= !disk_write; p0_write <= disk_write;
                p0_writedata <= {disk_wdata, disk_wdata};
                p0_byteenable <= disk_address[0] ? 16'hff00 : 16'h00ff;
            end else if (engine_request && !engine_done) begin
                owner <= ENGINE;
                p0_address <= engine_address; p0_read <= !engine_write; p0_write <= engine_write;
                p0_writedata <= engine_wdata; p0_byteenable <= engine_be;
            end
        end else if ((p0_read || p0_write) && !p0_waitrequest) begin
            // A write is done when the port takes it; a read when its beat returns.
            p0_read <= 0; p0_write <= 0;
            waiting <= p0_read;
            if (p0_write) begin
                owner <= NONE;
                case (owner)
                    DMA: begin dma_done <= 1; dma_started <= 0; end
                    DISK: begin disk_ready <= 1; disk_started <= 0; end
                    default: engine_done <= 1;
                endcase
            end else if (owner == DISK) disk_ready <= 1;
        end else if (waiting && p0_readdatavalid) begin
            waiting <= 0;
            owner <= NONE;
            case (owner)
                DMA: begin
                    dma_done <= 1; dma_started <= 0;
                    dma_rdata <= p0_readdata[{dma_address[3:0], 3'b000} +: 8];
                end
                DISK: begin
                    disk_rvalid <= 1; disk_started <= 0;
                    disk_rdata <= disk_half ? p0_readdata[127:64] : p0_readdata[63:0];
                end
                default: begin engine_done <= 1; read_beat <= p0_readdata; end
            endcase
        end
    end

    // ---- Command engine
    // A fetched command is checked, then loaded, a clock apart; waits count
    // down and each command's clocks count up, so no wide compare or subtract
    // sits on the command path. The check also decodes the command's fields
    // and ranges, so a command that would alias another or reach past its
    // range is refused before it runs.
    localparam [3:0] E_START = 4'd0, E_EPOCH = 4'd1, E_IDLE = 4'd2, E_BELL = 4'd3, E_FETCH = 4'd4,
        E_RUN = 4'd5, E_RESULT = 4'd6, E_STATUS = 4'd7, E_BEAT = 4'd8, E_ANNOUNCE = 4'd9,
        E_CHECK = 4'd10, E_LOAD = 4'd11;
    reg [3:0] state, after_beat;
    reg [31:0] epoch, head, tail, expected;
    reg [7:0] poll;
    reg [127:0] command;
    wire [7:0] op = command[7:0];
    wire [1:0] size = command[9:8];
    wire [15:0] count = command[31:16];
    wire [31:0] address = command[63:32], data = command[95:64];
    // Every command but these two ends within TIMEOUT_CLOCKS.
    wire bounded = op != OP_DELAY && op != OP_WAIT_INTR;
    reg [31:0] result, observed, elapsed, countdown, ddr_offset;
    reg [16:0] remaining;
    reg [16:2] mmio_address;
    reg [7:0] status, verdict;
    reg [2:0] step;
    reg [1:0] lane;
    reg [127:0] block;
    reg matched;

    // The fetched command's checks: status 2 for an op, index or size that
    // would alias another, 3 for an MMIO range outside A0000h-BFFFFh or a
    // window range outside the window.
    wire [7:0] fetched_op = read_beat[7:0];
    wire [15:0] fetched_count = read_beat[31:16];
    wire [31:0] fetched_address = read_beat[63:32], fetched_data = read_beat[95:64];
    wire fetched_io = fetched_op == OP_OUT || fetched_op == OP_IN || fetched_op == OP_POLL_IN;
    wire fetched_mmio = fetched_op >= OP_MMIO_WR && fetched_op <= OP_COPY_OUT;
    wire fetched_copy = fetched_op == OP_COPY_IN || fetched_op == OP_COPY_OUT;
    // FILL and the copies cover count dwords; MMIO_WR and MMIO_RD one.
    wire [16:0] mmio_end = {2'b00, fetched_address[16:2]} +
                           (fetched_op >= OP_FILL ? {1'b0, fetched_count} : 17'd1);
    wire [28:0] copy_end = {1'b0, fetched_data[27:0]} + {11'd0, fetched_count, 2'b00};
    wire known = fetched_op <= OP_KEY && !(fetched_io && read_beat[9:8] == 2'd3) &&
                 !(fetched_op == OP_CONFIG && fetched_address > 32'd12) &&
                 !(fetched_op == OP_READ && fetched_address > 32'd16);
    wire mmio_placed = fetched_address[31:17] == 15'h0005 && fetched_address[1:0] == 2'd0 && mmio_end <= 17'h08000;
    wire copy_placed = fetched_data[31:28] == 4'd0 && fetched_data[3:0] == 4'd0 && copy_end <= 29'h10000000;
    wire placed = (!fetched_mmio || mmio_placed) && (!fetched_copy || copy_placed);

    task automatic beat(input write, input [27:0] where, input [127:0] value, input [15:0] enables);
        engine_request <= 1; engine_write <= write; engine_address <= where;
        engine_wdata <= value; engine_be <= enables;
    endtask

    always @(posedge clk) begin
        if (engine_done) engine_request <= 0;
        if (reset) begin
            state <= E_START; engine_request <= 0; engine_write <= 0; engine_address <= 0;
            engine_wdata <= 0; engine_be <= 0; epoch <= 0; head <= 0; tail <= 0; expected <= 0; poll <= 0;
            command <= 0; result <= 0; observed <= 0; elapsed <= 0; countdown <= 0; remaining <= 0;
            ddr_offset <= 0; status <= 0; verdict <= 0; matched <= 0;
            step <= 0; lane <= 0; block <= 0; mmio_address <= 0; after_beat <= E_EPOCH;
            io_valid <= 0; io_write <= 0; inta_valid <= 0; io_port <= 0; io_size <= 0; io_wdata <= 0;
            mem_valid <= 0; mem_write <= 0; mem_address <= 0; mem_byteenable <= 0; mem_wdata <= 0;
            key_valid <= 0; key_code <= 0;
            machine_hold <= 1;
            base_kib <= 16'd640; extended_kib <= 16'd15360; above16m_64k <= 16'd0;
            sb_base_high <= 8'h22; sb_irq <= 8'd7; sb_dma <= 8'd1;
            disk_present <= 0; disk_sectors <= 0; disk_cylinders <= 0; disk_heads <= 0; disk_spt <= 0;
            dirty_index <= 0;
        end else begin
            io_valid <= 0;
            inta_valid <= 0;
            elapsed <= elapsed + 32'd1;
            if (countdown != 0) countdown <= countdown - 32'd1;
            case (state)
                // The epoch is the clock count when the master first runs.
                E_START: begin
                    epoch <= clock;
                    expected <= clock;
                    beat(1, DOORBELL, {64'd0, clock, 32'd0}, 16'hffff);
                    after_beat <= E_ANNOUNCE;
                    state <= E_BEAT;
                end
                E_ANNOUNCE: begin
                    beat(1, STATUS, {clock, epoch, MAGIC, 32'd0}, 16'hffff);
                    after_beat <= E_EPOCH;
                    state <= E_BEAT;
                end
                E_BEAT: if (engine_done) state <= after_beat;
                E_EPOCH, E_IDLE: if (poll == POLL_CLOCKS[7:0]) begin
                    poll <= 0;
                    beat(0, DOORBELL, 128'd0, 16'h0000);
                    state <= E_BELL;
                end else if (state == E_IDLE && head != tail) begin
                    beat(0, COMMANDS + {16'd0, head[11:0]}, 128'd0, 16'h0000);
                    state <= E_FETCH;
                end else poll <= poll + 8'd1;
                E_BELL: if (engine_done) begin
                    // A doorbell from another epoch is not ours yet.
                    if (read_beat[63:32] == epoch) tail <= read_beat[31:0];
                    state <= read_beat[63:32] == epoch ? E_IDLE : E_EPOCH;
                end
                E_FETCH: if (engine_done) state <= E_CHECK;
                E_CHECK: begin
                    matched <= read_beat[127:96] == expected;
                    observed <= read_beat[36] ? clock : observe[{read_beat[35:32], 5'b00000} +: 32];
                    verdict <= !known ? 8'd2 : !placed ? 8'd3 : 8'd0;
                    state <= E_LOAD;
                end
                E_LOAD: if (!matched) state <= E_IDLE;
                else begin
                    command <= read_beat;
                    elapsed <= 0;
                    countdown <= fetched_op == OP_DELAY || fetched_op == OP_WAIT_INTR ? fetched_data
                                                                                      : TIMEOUT_CLOCKS;
                    result <= 0; status <= verdict; step <= 0; lane <= 0;
                    remaining <= {1'b0, fetched_count};
                    ddr_offset <= fetched_data;
                    mmio_address <= read_beat[48:34];
                    state <= verdict == 8'd0 ? E_RUN : E_RESULT;
                end
                // A handshake the machine never answers, as while it is held in
                // reset, is given up. A port 0 beat never is: the machine's
                // reset does not reach port 0, and the arbiter sees it through.
                E_RUN: if (bounded && countdown == 0 && !engine_request) begin
                    mem_valid <= 0; key_valid <= 0;
                    status <= 8'd1;
                    state <= E_RESULT;
                end else case (op)
                    OP_NOP: state <= E_RESULT;
                    OP_OUT, OP_IN, OP_POLL_IN: case (step)
                        0: if (!io_busy) begin
                            io_valid <= 1; io_write <= op == OP_OUT; io_port <= count;
                            io_size <= size; io_wdata <= data;
                            step <= 1;
                        end
                        1: if (io_done) begin
                            result <= io_rdata;
                            if (op == OP_POLL_IN) step <= 2; else state <= E_RESULT;
                        end
                        default: if ((result & address) == data) state <= E_RESULT;
                            else step <= 0;
                    endcase
                    OP_INTA: case (step)
                        0: if (!io_busy) begin inta_valid <= 1; step <= 1; end
                        default: if (inta_done) begin result <= {24'd0, inta_vector}; state <= E_RESULT; end
                    endcase
                    OP_MMIO_WR, OP_MMIO_RD: case (step)
                        0: begin
                            mem_valid <= 1; mem_write <= op == OP_MMIO_WR; mem_address <= mmio_address;
                            mem_byteenable <= op == OP_MMIO_WR ? command[11:8] : 4'hf; mem_wdata <= data;
                            step <= 1;
                        end
                        1: if (mem_ready) begin
                            mem_valid <= 0;
                            if (op == OP_MMIO_WR) state <= E_RESULT; else step <= 2;
                        end
                        default: if (mem_rvalid) begin result <= mem_rdata; state <= E_RESULT; end
                    endcase
                    OP_FILL: if (remaining == 0) begin
                        if (!mem_valid || mem_ready) begin mem_valid <= 0; state <= E_RESULT; end
                    end else if (!mem_valid || mem_ready) begin
                        mem_valid <= 1; mem_write <= 1; mem_address <= mmio_address;
                        mem_byteenable <= 4'hf; mem_wdata <= data;
                        mmio_address <= mmio_address + 15'd1;
                        remaining <= remaining - 17'd1;
                    end
                    // Four dwords per beat from lane 0; the window offset is
                    // beat-aligned, and only its low 28 bits form the address.
                    OP_COPY_IN: case (step)
                        0: if (remaining == 0) state <= E_RESULT;
                        else begin
                            beat(0, BASE + {4'd0, ddr_offset[27:4]}, 128'd0, 16'h0000);
                            step <= 1;
                        end
                        1: if (engine_done) step <= 2;
                        // Step 3: all four lanes of this beat are issued.
                        default: if (!mem_valid || mem_ready) begin
                            if (remaining == 0 || step == 3) begin
                                mem_valid <= 0;
                                ddr_offset <= ddr_offset + 32'd16;
                                step <= 0;
                            end else begin
                                mem_valid <= 1; mem_write <= 1; mem_address <= mmio_address;
                                mem_byteenable <= 4'hf; mem_wdata <= read_beat[{lane, 5'b00000} +: 32];
                                mmio_address <= mmio_address + 15'd1;
                                remaining <= remaining - 17'd1;
                                lane <= lane + 2'd1;
                                if (lane == 2'd3) step <= 3;
                            end
                        end
                    endcase
                    OP_COPY_OUT: case (step)
                        0: if (remaining == 0) state <= E_RESULT;
                        else begin
                            mem_valid <= 1; mem_write <= 0; mem_address <= mmio_address; mem_byteenable <= 4'hf;
                            step <= 1;
                        end
                        1: if (mem_ready) begin mem_valid <= 0; step <= 2; end
                        2: if (mem_rvalid) begin
                            block[{lane, 5'b00000} +: 32] <= mem_rdata;
                            mmio_address <= mmio_address + 15'd1;
                            remaining <= remaining - 17'd1;
                            lane <= lane + 2'd1;
                            step <= lane == 2'd3 || remaining == 17'd1 ? 3'd3 : 3'd0;
                        end
                        // Only the lanes filled: lane has wrapped to 0 after all four.
                        3: begin
                            beat(1, BASE + {4'd0, ddr_offset[27:4]}, block,
                                 lane == 2'd0 ? 16'hffff : lane == 2'd1 ? 16'h000f : lane == 2'd2 ? 16'h00ff
                                                                                       : 16'h0fff);
                            step <= 4;
                        end
                        default: if (engine_done) begin
                            ddr_offset <= ddr_offset + 32'd16;
                            step <= 0;
                        end
                    endcase
                    OP_WAIT_INTR: if (intr) begin result <= 32'd1; state <= E_RESULT; end
                        else if (countdown == 0) state <= E_RESULT;
                    OP_DELAY: if (countdown == 0) state <= E_RESULT;
                    OP_CONFIG: begin
                        case (address[3:0])
                            4'd0: machine_hold <= data[0];
                            4'd1: base_kib <= data[15:0];
                            4'd2: extended_kib <= data[15:0];
                            4'd3: above16m_64k <= data[15:0];
                            4'd4: sb_base_high <= data[7:0];
                            4'd5: sb_irq <= data[7:0];
                            4'd6: sb_dma <= data[7:0];
                            4'd7: disk_present <= data[0];
                            4'd8: disk_sectors <= data;
                            4'd9: disk_cylinders <= data[15:0];
                            4'd10: disk_heads <= data[4:0];
                            4'd11: disk_spt <= data[5:0];
                            4'd12: dirty_index <= data[6:0];
                            default: ;
                        endcase
                        state <= E_RESULT;
                    end
                    OP_READ: begin
                        result <= observed;
                        state <= E_RESULT;
                    end
                    OP_KEY: if (key_valid && key_ready) begin key_valid <= 0; state <= E_RESULT; end
                        else begin key_valid <= 1; key_code <= data[7:0]; end
                    default: state <= E_RESULT;
                endcase
                E_RESULT: begin
                    beat(1, RESULTS + {16'd0, head[11:0]}, {expected, 24'd0, status, elapsed, result}, 16'hffff);
                    after_beat <= E_STATUS;
                    state <= E_BEAT;
                end
                E_STATUS: begin
                    beat(1, STATUS, {clock, epoch, MAGIC, head + 32'd1}, 16'hffff);
                    head <= head + 32'd1;
                    expected <= expected + 32'd1;
                    after_beat <= E_IDLE;
                    state <= E_BEAT;
                end
                default: state <= E_IDLE;
            endcase
        end
    end
endmodule
