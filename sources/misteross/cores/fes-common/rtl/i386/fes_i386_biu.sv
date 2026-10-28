// SPDX-License-Identifier: MIT
// The bus interface of fes_i386_cpu (docs/i386-cpu-design.md, "Stores and
// forwarding", "Memory types", "L2 and coherence" and "Machine interface"),
// as the first memory slice builds it: there is no L2 yet, so the L1s fill
// straight from HPS DDR port 0 and the write buffer drains there.
//
// The write buffer holds four 32-byte line entries with byte masks, in
// order. A store WB commits enters it on that edge: its bytes in RAM coalesce
// into the newest entry when that is the same line and not yet being
// written, else take a new entry; its bytes in MMIO take an entry of their
// own, never coalesced, so each store reaches the MMIO port as the
// instruction made it. Bytes in read-only RAM or a hole are dropped. A store
// whose lanes run past its line takes a second entry for the next line. So
// a store needs two free entries; the CPU holds a store in WB while the
// buffer will not have them (wb_room_next).
//
// The oldest entry is written once a newer one exists, once it is full, an
// MMIO store, or two cycles old, or whenever something waits for the buffer:
// a read, DMA, FLUSH or an I/O access (drain). A RAM entry is one two-beat
// burst on port 0 with byte enables; an MMIO entry is one MMIO write per
// dword it has bytes in, lowest first.
//
// Reads are served one at a time, the L1D's first, then DMA's, then the
// L1I's. A line read (two beats) waits until no entry holds bytes of its
// line, so DDR has every committed store of it; an MMIO read waits until
// the buffer is empty, so every older store has reached its device. A DMA
// byte waits as a line read does, and a DMA write then invalidates the line's
// set in both L1s (every way: DMA writes are rare). FLUSH empties the buffer
// and then invalidates both L1s. A CPU reset (reset) leaves the buffer to
// drain and lets a read in flight finish unseen; cold_reset clears it all.
//
// Beats reach the requester as port 0 returns them (rdata, with d_beat or
// i_beat); a requester holds its request until its last beat.
module fes_i386_biu import fes_i386_pkg::*; #(
    parameter logic [31:0] RAM_BASE = 32'h3400_0000  // the HPS address of guest physical 0
) (
    input  logic             clk,
    input  logic             cold_reset,
    input  logic             reset,
    input  logic [23:0][1:0] attr_upper,
    input  logic [6:0]       ram_top,
    // The store WB commits: the bytes st_mask names of the 8 from dword
    // st_dword of line st_line0, lanes past dword 7 in line st_line1.
    input  logic             st_valid,
    input  logic [PA-6:0]    st_line0,
    input  logic [PA-6:0]    st_line1,
    input  attr_e            st_attr0,
    input  attr_e            st_attr1,
    input  logic [2:0]       st_dword,
    input  logic [7:0][7:0]  st_data,
    input  logic [7:0]       st_mask,
    output logic             wb_room_next,  // two entries free next cycle
    output logic             wb_empty,      // every committed store has left the buffer
    input  logic             drain,
    // The L1D: a line read (d_mmio clear: d_addr's line), or one MMIO dword
    // read with byte enables d_be.
    input  logic             d_req,
    input  logic             d_mmio,
    input  logic [PA-3:0]    d_addr,
    input  logic [3:0]       d_be,
    output logic             d_beat,
    // The L1I: a line read.
    input  logic             i_req,
    input  logic [PA-6:0]    i_line,
    output logic             i_beat,
    output logic [127:0]     rdata,         // a beat: 16 bytes of a line, or the MMIO dword
    // Both L1s: invalidate every way of inv_line's set, or every line.
    output logic             inv_valid,
    output logic [PA-6:0]    inv_line,
    output logic             inv_all,
    // HPS DDR port 0 (fes.memory.hps-ddr): word addresses, 128-bit beats.
    output logic [27:0]      ddr_address,
    output logic [7:0]       ddr_burstcount,
    output logic             ddr_read,
    output logic             ddr_write,
    output logic [127:0]     ddr_writedata,
    output logic [15:0]      ddr_byteenable,
    input  logic             ddr_waitrequest,
    input  logic [127:0]     ddr_readdata,
    input  logic             ddr_readdatavalid,
    // MMIO: dword reads (blocking) and posted writes, one a cycle when ready.
    output logic             mmio_valid,
    output logic             mmio_write,
    output logic [PA-3:0]    mmio_address,
    output logic [3:0]       mmio_byteenable,
    output logic [31:0]      mmio_wdata,
    input  logic             mmio_ready,
    input  logic             mmio_rvalid,
    input  logic [31:0]      mmio_rdata,
    // Machine DMA: one physical byte, held until dma_done.
    input  logic             dma_valid,
    input  logic             dma_write,
    input  logic [23:0]      dma_address,
    input  logic [7:0]       dma_wdata,
    output logic             dma_done,
    output logic [7:0]       dma_rdata,
    // FLUSH, held until flush_done.
    input  logic             flush_request,
    output logic             flush_done,
    output logic             idle            // nothing buffered, nothing in flight
);
    localparam int LW = PA - 5;  // line address bits
    localparam logic [27:0] BASE_WORD = RAM_BASE[31:4];

    // ---- The write buffer ---------------------------------------------------------
    logic [3:0]            e_mmio;
    logic [3:0][LW-1:0]    e_line;
    logic [3:0][31:0]      e_mask;
    logic [3:0][31:0][7:0] e_data;
    logic [1:0]            head;
    logic [2:0]            count;
    logic [1:0]            age;      // cycles since the newest entry last took bytes
    logic                  writing;  // the head entry is being written: it takes no more bytes

    logic [1:0] tail, tail1;
    assign tail = head + 2'(count) - 2'd1;
    assign tail1 = head + 2'(count);

    // The store's two parts, placed in their lines.
    logic             part0, part1, merge0, take0, take1;
    logic [31:0]      mask0, mask1;
    logic [31:0][7:0] data0, data1;
    assign mask0 = 32'({24'd0, st_mask} << {st_dword, 2'b00});
    assign data0 = 256'({192'd0, st_data} << {st_dword, 5'b00000});
    assign mask1 = {28'd0, st_dword == 3'd7 ? st_mask[7:4] : 4'd0};
    assign data1 = {224'd0, st_data[7:4]};
    assign part0 = st_valid && mask0 != 32'd0 && (st_attr0 == ATTR_RAM || st_attr0 == ATTR_MMIO);
    assign part1 = st_valid && mask1 != 32'd0 && (st_attr1 == ATTR_RAM || st_attr1 == ATTR_MMIO);
    assign merge0 = part0 && st_attr0 == ATTR_RAM && count != 3'd0 && !e_mmio[tail]
                 && e_line[tail] == st_line0 && !(writing && count == 3'd1);
    assign take0 = part0 && !merge0;
    assign take1 = part1;

    // An entry holding bytes of a line (RAM entries only).
    function automatic logic held(input logic [LW-1:0] line, input logic [1:0] h, input logic [2:0] n,
                                  input logic [3:0] mmio, input logic [3:0][LW-1:0] lines);
        held = 1'b0;
        for (int k = 0; k < 4; k++)
            if (3'(2'(k - h)) < n && !mmio[k] && lines[k] == line) held = 1'b1;
    endfunction

    // Each entry takes the store's first part, its second, or the first's
    // bytes merged in; written entry by entry with constant indices, which the
    // locked Yosys reads right (a byte loop into e_data[tail] it does not).
    logic [1:0] tail2;
    assign tail2 = take0 ? tail1 + 2'd1 : tail1;
    for (genvar k = 0; k < 4; k++) begin : g_entry
        always_ff @(posedge clk) begin
            if (!cold_reset && take1 && tail2 == 2'(k)) begin
                e_mmio[k] <= st_attr1 == ATTR_MMIO;
                e_line[k] <= st_line1;
                e_mask[k] <= mask1;
                e_data[k] <= data1;
            end else if (!cold_reset && take0 && tail1 == 2'(k)) begin
                e_mmio[k] <= st_attr0 == ATTR_MMIO;
                e_line[k] <= st_line0;
                e_mask[k] <= mask0;
                e_data[k] <= data0;
            end else if (!cold_reset && merge0 && tail == 2'(k)) begin
                for (int b = 0; b < 32; b++) if (mask0[b]) e_data[k][b] <= data0[b];
                e_mask[k] <= e_mask[k] | mask0;
            end
        end
    end

    // ---- Requests -------------------------------------------------------------
    // DMA's request, latched while it is served.
    logic          dma_busy;
    logic [23:0]   dma_a;
    logic          dma_w;
    logic [7:0]    dma_wd;
    logic [PA:0]   dma_p;
    assign dma_p = {{(PA-23){1'b0}}, dma_a};
    logic          dma_ram;
    assign dma_ram = attr(dma_p, attr_upper, ram_top) == ATTR_RAM;

    logic d_ok, i_ok, dma_ok;
    assign d_ok = d_req && !reset && (d_mmio ? count == 3'd0 : !held(d_addr[PA-3:3], head, count, e_mmio, e_line));
    assign dma_ok = dma_busy && dma_ram && !held(dma_p[PA-1:5], head, count, e_mmio, e_line);
    assign i_ok = i_req && !reset && !held(i_line, head, count, e_mmio, e_line);

    logic eligible;
    assign eligible = count != 3'd0 && (count >= 3'd2 || e_mmio[head] || &e_mask[head] || age == 2'd2 || drain
                                        || flush_request || d_req || i_req || dma_busy);

    // ---- The port sequencer -------------------------------------------------------
    typedef enum logic [3:0] {
        S_IDLE, S_RCMD, S_RDATA, S_W0, S_W1, S_MW, S_MRCMD, S_MRDATA, S_DW, S_DONE
    } state_e;
    typedef enum logic [1:0] { WHO_D, WHO_I, WHO_DMA, WHO_NONE } who_e;
    state_e        state;
    who_e          who;
    logic          beat_n;      // the next beat of a line read is its second
    logic          single;      // a one-beat read (DMA)
    logic [7:0]    mw_left;     // an MMIO entry's dwords not yet written
    logic [2:0]    mw_k;        // the lowest of them

    always_comb begin
        mw_k = 3'd0;
        for (int k = 7; k >= 0; k--) if (mw_left[k]) mw_k = 3'(k);
    end
    logic [7:0] head_dwords;
    for (genvar k = 0; k < 8; k++) begin : g_dw
        assign head_dwords[k] = |e_mask[head][4 * k +: 4];
    end

    logic pop;
    assign pop = state == S_W1 && !ddr_waitrequest
              || state == S_MW && mmio_ready && (mw_left & ~(8'd1 << mw_k)) == 8'd0;

    assign d_beat = who == WHO_D && (state == S_RDATA && ddr_readdatavalid || state == S_MRDATA && mmio_rvalid);
    assign i_beat = who == WHO_I && state == S_RDATA && ddr_readdatavalid;
    assign rdata = state == S_MRDATA ? {96'd0, mmio_rdata} : ddr_readdata;

    // Port outputs, from the state.
    always_comb begin
        ddr_read = state == S_RCMD;
        ddr_write = state == S_W0 || state == S_W1 || state == S_DW;
        ddr_burstcount = single || state == S_DW ? 8'd1 : 8'd2;
        ddr_writedata = state == S_W1 ? e_data[head][31:16] : state == S_DW ? {16{dma_wd}} : e_data[head][15:0];
        ddr_byteenable = state == S_W1 ? e_mask[head][31:16] : state == S_DW ? 16'(16'd1 << dma_a[3:0])
                       : e_mask[head][15:0];
        mmio_valid = state == S_MW || state == S_MRCMD;
        mmio_write = state == S_MW;
        mmio_address = state == S_MW ? {e_line[head], mw_k} : d_addr;
        mmio_byteenable = state == S_MW ? e_mask[head][4 * mw_k +: 4] : d_be;
        mmio_wdata = e_data[head][4 * mw_k +: 4];
    end
    logic [27:0] rd_address, wr_address;
    always_ff @(posedge clk) begin
        // The read's or write's word address, registered as its command starts.
        if (state == S_IDLE) begin
            wr_address <= BASE_WORD + {{(28-LW-1){1'b0}}, e_line[head], 1'b0};
        end
    end
    assign ddr_address = state == S_W0 || state == S_W1 ? wr_address : rd_address;

    always_ff @(posedge clk) begin
        inv_valid <= 1'b0;
        inv_all <= 1'b0;
        dma_done <= 1'b0;
        flush_done <= 1'b0;
        if (cold_reset) begin
            state <= S_IDLE;
            who <= WHO_NONE;
            count <= 3'd0;
            head <= 2'd0;
            age <= 2'd0;
            writing <= 1'b0;
            dma_busy <= 1'b0;
        end else begin
            // A reset leaves a read in flight to finish unseen.
            if (reset && (who == WHO_D || who == WHO_I)) who <= WHO_NONE;
            if (dma_valid && !dma_busy && !dma_done) begin
                dma_busy <= 1'b1;
                dma_a <= dma_address;
                dma_w <= dma_write;
                dma_wd <= dma_wdata;
            end

            // The buffer: pops at the head, the store's entries at the tail.
            if (part0 || part1) age <= 2'd0;
            else if (age != 2'd2) age <= age + 2'd1;
            count <= count - 3'(pop) + 3'(take0) + 3'(take1);
            if (pop) begin
                head <= head + 2'd1;
                writing <= 1'b0;
            end

            case (state)
                S_IDLE: begin
                    single <= 1'b0;
                    beat_n <= 1'b0;
                    if (d_ok) begin
                        who <= WHO_D;
                        state <= d_mmio ? S_MRCMD : S_RCMD;
                        rd_address <= BASE_WORD + {{(28-LW-1){1'b0}}, d_addr[PA-3:3], 1'b0};
                    end else if (dma_busy && !dma_ram) begin
                        // Not RAM: reads FFh, a write is dropped.
                        dma_rdata <= 8'hff;
                        dma_done <= 1'b1;
                        dma_busy <= 1'b0;
                    end else if (dma_ok) begin
                        who <= WHO_DMA;
                        single <= 1'b1;
                        state <= dma_w ? S_DW : S_RCMD;
                        rd_address <= BASE_WORD + {{(28-PA+4){1'b0}}, dma_p[PA-1:4]};
                    end else if (i_ok) begin
                        who <= WHO_I;
                        state <= S_RCMD;
                        rd_address <= BASE_WORD + {{(28-LW-1){1'b0}}, i_line, 1'b0};
                    end else if (eligible) begin
                        who <= WHO_NONE;
                        writing <= 1'b1;
                        mw_left <= head_dwords;
                        state <= e_mmio[head] ? S_MW : S_W0;
                    end else if (flush_request && count == 3'd0 && !flush_done) begin
                        inv_all <= 1'b1;
                        flush_done <= 1'b1;
                    end
                end
                S_RCMD: if (!ddr_waitrequest) state <= S_RDATA;
                S_RDATA: if (ddr_readdatavalid) begin
                    beat_n <= 1'b1;
                    if (who == WHO_DMA) dma_rdata <= ddr_readdata[8 * dma_a[3:0] +: 8];
                    if (beat_n || single) begin
                        state <= S_IDLE;
                        who <= WHO_NONE;
                        if (who == WHO_DMA) begin
                            dma_done <= 1'b1;
                            dma_busy <= 1'b0;
                        end
                    end
                end
                S_W0: if (!ddr_waitrequest) state <= S_W1;
                S_W1: if (!ddr_waitrequest) state <= S_IDLE;
                S_MW: if (mmio_ready) begin
                    mw_left[mw_k] <= 1'b0;
                    if (pop) state <= S_IDLE;
                end
                S_MRCMD: if (mmio_ready) state <= S_MRDATA;
                S_MRDATA: if (mmio_rvalid) begin
                    state <= S_IDLE;
                    who <= WHO_NONE;
                end
                S_DW: if (!ddr_waitrequest) begin
                    state <= S_IDLE;
                    who <= WHO_NONE;
                    dma_done <= 1'b1;
                    dma_busy <= 1'b0;
                    inv_valid <= 1'b1;
                    inv_line <= dma_p[PA-1:5];
                end
                default: state <= S_IDLE;
            endcase
            // A reset abandons an MMIO read the machine has not taken.
            if (reset && state == S_MRCMD && who == WHO_D && !mmio_ready) state <= S_IDLE;
        end
    end

    // Room for a store's two entries next cycle, and the buffer empty now.
    logic [2:0] count_next;
    assign count_next = count - 3'(pop) + 3'(take0) + 3'(take1);
    assign wb_room_next = count_next <= 3'd2;
    assign wb_empty = count == 3'd0;
    assign idle = count == 3'd0 && state == S_IDLE && !dma_busy;
endmodule
