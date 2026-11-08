// SPDX-License-Identifier: MIT
// pc386 EMS board (docs/pc386-machine-design.md, "EMS board"): the Lo-tech
// 2MB EMS Board's register interface with its jumpers at E000h and 260h, and
// the hkzlab 4 MB board's 8-bit page numbers.
//
// Four write-only page registers, written by the I/O decoder for ports
// 260h-263h. Frame page n (E0000h + n * 4000h) shows board page register[n];
// a register at or above PAGES leaves its frame page unmapped (reads FFh,
// writes dropped). Machine reset sets every register to FFh, which is always
// unmapped. With PAGES 0 the board is not fitted and the frame is never
// decoded here (the attribute map makes it RAM).
//
// Frame MMIO: dword offsets within E0000h-EFFFFh. The board page is chosen
// when an access is taken, so a page-register write that follows a posted
// frame store (the CPU runs OUT after its stores have reached their device)
// never moves it. Writes are taken in a clock and become one masked 64-bit
// beat on port 2; a read is taken, sent as a one-beat read and answered with
// rvalid. One access is in flight at a time.
//
// The 8237's memory cycles into the frame come here too, as on an ISA bus
// where the board decodes them: one byte each, taken before a waiting MMIO
// access, answered with dma_done for a clock (dma_rdata with it for a read).
//
// Board memory is BASE (a 64-bit word address on port 2) plus page * 2048
// words: window offset 01400000h, 4 MiB reserved.
module pc386_ems #(
    parameter [28:0] BASE = 29'h06280000
) (
    input wire clk, reset,
    input wire [7:0] pages,                 // board size in 16 KiB pages, 0 when not fitted
    // Page register writes from the I/O decoder
    input wire reg_write,
    input wire [1:0] reg_index,
    input wire [7:0] reg_wdata,
    // Frame MMIO
    input wire mem_valid, mem_write,
    input wire [15:2] mem_address,
    input wire [3:0] mem_byteenable,
    input wire [31:0] mem_wdata,
    output wire mem_ready,
    output reg mem_rvalid,
    output reg [31:0] mem_rdata,
    // DMA: byte offsets within E0000h-EFFFFh, valid held until done
    input wire dma_valid, dma_write,
    input wire [15:0] dma_address,
    input wire [7:0] dma_wdata,
    output reg dma_done,
    output reg [7:0] dma_rdata,
    // Port 2 master: single beats
    output reg [28:0] p2_address,
    output reg p2_read, p2_write,
    output reg [63:0] p2_writedata,
    output reg [7:0] p2_byteenable,
    input wire p2_waitrequest,
    input wire [63:0] p2_readdata,
    input wire p2_readdatavalid
);
    reg [7:0] page_reg [0:3];
    localparam [1:0] IDLE = 2'd0, COMMAND = 2'd1, WAIT = 2'd2;
    reg [1:0] state;
    reg high_half;                          // the read's dword is bits 63:32 of its beat
    reg is_dma;                             // the access in flight is a DMA cycle
    reg [2:0] dma_lane;                     // its byte within the beat

    // A DMA cycle goes first; dma_done is the clock it ends, when valid is still up.
    wire dma_take = state == IDLE && dma_valid && !dma_done;
    wire [15:0] offset = dma_take ? dma_address : {mem_address, 2'b00};
    wire [7:0] page = page_reg[offset[15:14]];
    wire mapped = page < pages;
    assign mem_ready = state == IDLE && !(dma_valid && !dma_done);
    wire taken = mem_valid && mem_ready;
    wire access_write = dma_take ? dma_write : mem_write;

    integer n;
    always @(posedge clk) begin
        if (reset) begin
            for (n = 0; n < 4; n = n + 1) page_reg[n] <= 8'hff;
            state <= IDLE;
            p2_read <= 0; p2_write <= 0; p2_address <= 0; p2_writedata <= 0; p2_byteenable <= 0;
            mem_rvalid <= 0; mem_rdata <= 0; high_half <= 0; is_dma <= 0; dma_lane <= 0;
            dma_done <= 0; dma_rdata <= 0;
        end else begin
            if (reg_write) page_reg[reg_index] <= reg_wdata;
            mem_rvalid <= 0;
            dma_done <= 0;
            case (state)
                IDLE: if (dma_take || taken) begin
                    high_half <= offset[2];
                    is_dma <= dma_take;
                    dma_lane <= offset[2:0];
                    if (!mapped) begin
                        // Unmapped: a read answers FFh in the next clock, a write is dropped.
                        if (dma_take) begin dma_done <= 1; dma_rdata <= 8'hff; end
                        else if (!mem_write) begin mem_rvalid <= 1; mem_rdata <= 32'hffffffff; end
                    end else begin
                        p2_address <= BASE + {10'b0, page, offset[13:3]};
                        p2_read <= !access_write;
                        p2_write <= access_write;
                        p2_writedata <= dma_take ? {8{dma_wdata}} : {mem_wdata, mem_wdata};
                        p2_byteenable <= dma_take ? 8'b1 << offset[2:0] :
                                         offset[2] ? {mem_byteenable, 4'b0} : {4'b0, mem_byteenable};
                        state <= COMMAND;
                    end
                end
                COMMAND: if (!p2_waitrequest) begin
                    p2_read <= 0;
                    p2_write <= 0;
                    if (!p2_read && is_dma) dma_done <= 1;
                    state <= p2_read ? WAIT : IDLE;
                end
                WAIT: if (p2_readdatavalid) begin
                    if (is_dma) begin
                        dma_done <= 1;
                        dma_rdata <= p2_readdata[8 * dma_lane +: 8];
                    end else begin
                        mem_rvalid <= 1;
                        mem_rdata <= high_half ? p2_readdata[63:32] : p2_readdata[31:0];
                    end
                    state <= IDLE;
                end
                default: state <= IDLE;
            endcase
        end
    end
endmodule
