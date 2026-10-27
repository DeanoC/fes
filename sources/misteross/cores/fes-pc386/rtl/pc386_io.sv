// SPDX-License-Identifier: MIT
// pc386 I/O decoder and ISA timing (docs/pc386-machine-design.md, "I/O timing").
//
// The CPU issues one IN or OUT of a byte, word or dword. The decoder splits it
// into device cycles as the AT's bus controller does: the ATA data port 1F0h
// takes 16-bit cycles; every other port takes byte cycles at successive
// addresses. Each cycle lasts its timing class, counted in ISA clocks from a
// rational enable restarted at the cycle's start, so the figure is exact at
// any sealed clock:
//
//   isa8   6 ISA clocks (720 ns at 8.33 MHz): every on-board chip, unclaimed ports
//   isa16  3 ISA clocks (360 ns): the ATA data port
//   vga    3B0h-3DFh: handed to the VGA port; done when the VGA says so
//
// Within a cycle the chip select is held for phases 0-17, RD/WR (or INTA) is
// low for phases 1-8 and native devices see a one-clock access at phase 8,
// where read data is sampled. The imported 8259 and 8254 retire on the
// strobe's release, as on the PC/XT motherboard. INTA runs two isa8 acknowledge
// cycles and returns the vector the PICs drive on the second.
module pc386_io #(
    parameter integer ISA_N = 1,
    parameter integer ISA_D = 12
) (
    input wire clk, reset,
    // CPU I/O: a request is taken while !busy; done pulses once with the data.
    input wire io_valid, io_write,
    input wire [15:0] io_port,
    input wire [1:0] io_size,          // 0 byte, 1 word, 2 dword
    input wire [31:0] io_wdata,
    input wire inta_valid,
    output wire busy,
    output reg io_done, inta_done,
    output reg [31:0] io_rdata,
    output reg [7:0] inta_vector,
    // The current device cycle, stable for its whole length.
    output reg [15:0] cycle_port,
    output reg cycle_write, cycle_word, cycle_inta,
    output reg [15:0] cycle_wdata,
    output wire cycle_cs, cycle_strobe, cycle_access,
    input wire [15:0] cycle_rdata,
    // VGA ports: one byte per handshake, valid held until done.
    output reg vga_valid,
    input wire vga_done,
    input wire [7:0] vga_rdata
);
    localparam IDLE = 2'd0, CYCLE = 2'd1, VGA = 2'd2, DONE = 2'd3;
    localparam [5:0] ACCESS_PHASE = 6'd8, RELEASE_PHASE = 6'd17;
    localparam integer AW = $clog2(ISA_D + ISA_N + 1);

    reg [1:0] state;
    reg [15:0] base;
    reg [2:0] remaining;
    reg [1:0] index;
    reg inta, vga_gap;
    reg [31:0] wdata, rdata;
    reg [5:0] phase;
    reg [AW-1:0] accumulator;
    reg [2:0] isa_clocks;

    wire [AW-1:0] sum = accumulator + AW'(ISA_N);
    wire isa_tick = sum >= AW'(ISA_D);
    wire [2:0] isa_next = isa_clocks + {2'b0, isa_tick};
    wire [2:0] width = cycle_word ? 3'd2 : 3'd1;
    wire cycle_over = state == CYCLE && isa_next == (cycle_word ? 3'd3 : 3'd6) && phase >= RELEASE_PHASE;
    // The cycle after this one, or the first one of a new request.
    wire starting = state == IDLE;
    wire [1:0] next_index = starting ? 2'd0 : index + (state == VGA ? 2'd1 : width[1:0]);
    wire [2:0] next_remaining = starting ? (inta_valid ? 3'd2 : io_size == 0 ? 3'd1 : io_size == 1 ? 3'd2 : 3'd4) :
                                remaining - (state == VGA ? 3'd1 : width);
    wire next_inta = starting ? inta_valid : inta;
    wire [15:0] next_port = (starting ? (inta_valid ? 16'h0 : io_port) : base) + {14'b0, next_index};
    wire [31:0] next_wdata = starting ? io_wdata : wdata;
    wire next_word = !next_inta && next_port == 16'h01f0 && next_remaining >= 2;
    wire next_vga = !next_inta && next_port >= 16'h03b0 && next_port <= 16'h03df;

    assign busy = state != IDLE;
    assign cycle_cs = state == CYCLE && phase <= RELEASE_PHASE;
    assign cycle_strobe = state == CYCLE && phase >= 1 && phase <= ACCESS_PHASE;
    assign cycle_access = state == CYCLE && phase == ACCESS_PHASE;

    task automatic begin_cycle;
        index <= next_index;
        remaining <= next_remaining;
        cycle_port <= next_port;
        cycle_word <= next_word;
        cycle_wdata <= 16'(next_wdata >> (8 * next_index));
        phase <= 0;
        accumulator <= 0;
        isa_clocks <= 0;
        vga_valid <= next_vga;
        state <= next_vga ? VGA : CYCLE;
    endtask

    always @(posedge clk) begin
        io_done <= 0;
        inta_done <= 0;
        if (reset) begin
            state <= IDLE;
            vga_valid <= 0;
            cycle_port <= 0; cycle_write <= 0; cycle_word <= 0; cycle_inta <= 0; cycle_wdata <= 0;
            base <= 0; remaining <= 0; index <= 0; inta <= 0; vga_gap <= 0; wdata <= 0; rdata <= 0;
            phase <= 0; accumulator <= 0; isa_clocks <= 0;
            io_rdata <= 0; inta_vector <= 0;
        end else case (state)
            IDLE: if (inta_valid || io_valid) begin
                // INTA comes only at an instruction boundary, never with I/O.
                inta <= inta_valid;
                cycle_inta <= inta_valid;
                cycle_write <= !inta_valid && io_write;
                base <= inta_valid ? 16'h0 : io_port;
                wdata <= io_wdata;
                rdata <= 32'hffffffff;
                begin_cycle();
            end
            CYCLE: begin
                if (phase != 6'h3f) phase <= phase + 1'b1;
                accumulator <= isa_tick ? sum - AW'(ISA_D) : sum;
                isa_clocks <= isa_next;
                if (cycle_access) begin
                    if (cycle_word) rdata[8 * index +: 16] <= cycle_rdata;
                    else rdata[8 * index +: 8] <= cycle_rdata[7:0];
                    if (inta && index == 1) inta_vector <= cycle_rdata[7:0];
                end
                if (cycle_over) begin
                    if (remaining == width) state <= DONE;
                    else begin_cycle();
                end
            end
            // vga_valid drops for a clock between bytes, so each byte is a
            // fresh request.
            VGA: if (vga_gap) begin
                vga_gap <= 0;
                begin_cycle();
            end else if (vga_done) begin
                rdata[8 * index +: 8] <= vga_rdata;
                vga_valid <= 0;
                if (remaining == 1) state <= DONE;
                else vga_gap <= 1;
            end
            DONE: begin
                io_rdata <= rdata;
                io_done <= !inta;
                inta_done <= inta;
                state <= IDLE;
            end
        endcase
    end
endmodule
