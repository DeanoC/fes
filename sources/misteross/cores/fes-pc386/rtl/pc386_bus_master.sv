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
// After reset the master writes a status beat with a fresh epoch (the clock
// count at reset release) and waits until the doorbell carries that epoch.
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
// size: 0 byte, 1 word, 2 dword. Result status: 0 done, 1 timed out, 2 bad op.
// CONFIG 0 releases (0) or holds (1) the machine in reset; the master starts
// with it held so that the straps below are set first. CONFIG 1-11 set
// base_kib, extended_kib, above16m_64k, sb_base_high, sb_irq, sb_dma,
// disk_present, disk_sectors, disk_cylinders, disk_heads and disk_spt; 12 sets
// dirty_index. READ 0-15 return the observe words, READ 16 the clock count.
module pc386_bus_master #(
    parameter [31:0] WINDOW_BASE = 32'h30000000,
    parameter [31:0] MAGIC = 32'h33384d34,              // "4M83"
    parameter integer POLL_CLOCKS = 255,
    parameter [31:0] POLL_TIMEOUT = 32'h08000000
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
    localparam [3:0] OP_NOP = 4'd0, OP_OUT = 4'd1, OP_IN = 4'd2, OP_MMIO_WR = 4'd3, OP_MMIO_RD = 4'd4,
        OP_FILL = 4'd5, OP_COPY_IN = 4'd6, OP_COPY_OUT = 4'd7, OP_INTA = 4'd8, OP_WAIT_INTR = 4'd9,
        OP_DELAY = 4'd10, OP_POLL_IN = 4'd11, OP_CONFIG = 4'd12, OP_READ = 4'd13, OP_KEY = 4'd14;
    assign p0_burstcount = 8'd1;

    reg [31:0] clock;
    always @(posedge clk) clock <= reset ? 32'd0 : clock + 32'd1;

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
    localparam [3:0] E_START = 4'd0, E_EPOCH = 4'd1, E_IDLE = 4'd2, E_BELL = 4'd3, E_FETCH = 4'd4,
        E_RUN = 4'd5, E_RESULT = 4'd6, E_STATUS = 4'd7, E_BEAT = 4'd8;
    reg [3:0] state, after_beat;
    reg [31:0] epoch, head, tail;
    reg [7:0] poll;
    reg [127:0] command;
    wire [3:0] op = command[3:0];
    wire [1:0] size = command[9:8];
    wire [15:0] count = command[31:16];
    wire [31:0] address = command[63:32], data = command[95:64];
    reg [31:0] result, started, remaining, ddr_offset;
    reg [16:2] mmio_address;
    reg [7:0] status;
    reg [2:0] step;
    reg [1:0] lane;
    reg [127:0] block;
    wire [31:0] expected = epoch + head;

    task automatic beat(input write, input [27:0] where, input [127:0] value, input [15:0] enables);
        engine_request <= 1; engine_write <= write; engine_address <= where;
        engine_wdata <= value; engine_be <= enables;
    endtask

    always @(posedge clk) begin
        if (engine_done) engine_request <= 0;
        if (reset) begin
            state <= E_START; engine_request <= 0; engine_write <= 0; engine_address <= 0;
            engine_wdata <= 0; engine_be <= 0; epoch <= 0; head <= 0; tail <= 0; poll <= 0;
            command <= 0; result <= 0; started <= 0; remaining <= 0; ddr_offset <= 0; status <= 0;
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
            case (state)
                // The epoch is the clock count when the master first runs.
                E_START: begin
                    epoch <= clock;
                    beat(1, STATUS, {clock, clock, MAGIC, 32'd0}, 16'hffff);
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
                E_FETCH: if (engine_done) begin
                    if (read_beat[127:96] != expected) state <= E_IDLE;
                    else begin
                        command <= read_beat;
                        started <= clock;
                        result <= 0; status <= 0; step <= 0;
                        remaining <= {16'd0, read_beat[31:16]};
                        ddr_offset <= read_beat[95:64];
                        mmio_address <= read_beat[48:34];
                        state <= E_RUN;
                    end
                end
                E_RUN: case (op)
                    OP_NOP: state <= E_RESULT;
                    OP_OUT, OP_IN, OP_POLL_IN: case (step)
                        0: if (!io_busy) begin
                            io_valid <= 1; io_write <= op == OP_OUT; io_port <= count;
                            io_size <= size; io_wdata <= data;
                            step <= 1;
                        end
                        default: if (io_done) begin
                            result <= io_rdata;
                            if (op != OP_POLL_IN || (io_rdata & address) == data) state <= E_RESULT;
                            else if (clock - started > POLL_TIMEOUT) begin status <= 8'd1; state <= E_RESULT; end
                            else step <= 0;
                        end
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
                        remaining <= remaining - 32'd1;
                    end
                    // Four dwords per beat; the window offset is beat-aligned.
                    OP_COPY_IN: case (step)
                        0: if (remaining == 0) state <= E_RESULT;
                        else begin
                            beat(0, BASE + ddr_offset[31:4], 128'd0, 16'h0000);
                            lane <= 0;
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
                                remaining <= remaining - 32'd1;
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
                            remaining <= remaining - 32'd1;
                            lane <= lane + 2'd1;
                            step <= lane == 2'd3 || remaining == 32'd1 ? 3'd3 : 3'd0;
                        end
                        3: begin
                            beat(1, BASE + ddr_offset[31:4], block, 16'hffff);
                            step <= 4;
                        end
                        default: if (engine_done) begin
                            ddr_offset <= ddr_offset + 32'd16;
                            lane <= 0;
                            step <= 0;
                        end
                    endcase
                    OP_WAIT_INTR: if (intr) begin result <= 32'd1; state <= E_RESULT; end
                        else if (clock - started >= data) state <= E_RESULT;
                    OP_DELAY: if (clock - started >= data) state <= E_RESULT;
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
                            default: status <= 8'd2;
                        endcase
                        state <= E_RESULT;
                    end
                    OP_READ: begin
                        result <= address[4] ? clock : observe[{address[3:0], 5'b00000} +: 32];
                        state <= E_RESULT;
                    end
                    OP_KEY: if (key_valid && key_ready) begin key_valid <= 0; state <= E_RESULT; end
                        else begin key_valid <= 1; key_code <= data[7:0]; end
                    default: begin status <= 8'd2; state <= E_RESULT; end
                endcase
                E_RESULT: begin
                    beat(1, RESULTS + {16'd0, head[11:0]}, {expected, 24'd0, status, clock - started, result},
                         16'hffff);
                    after_beat <= E_STATUS;
                    state <= E_BEAT;
                end
                E_STATUS: begin
                    beat(1, STATUS, {clock, epoch, MAGIC, head + 32'd1}, 16'hffff);
                    head <= head + 32'd1;
                    after_beat <= E_IDLE;
                    state <= E_BEAT;
                end
                default: state <= E_IDLE;
            endcase
        end
    end
endmodule
