// SPDX-License-Identifier: GPL-2.0-or-later
// Real 8088 + motherboard + registered ISA display cards. Storage/font external.
module pcxt_machine #(parameter bit DIAGNOSTIC_PROBE = 0, SEPARATE_CPU_CLOCK = 0,
    parameter integer BOARD_CLOCK_HZ = 100000000) (
    input wire clk, clk_cpu, reset,
    output wire memory_valid,
    input wire memory_ready, memory_done,
    input wire [7:0] memory_rdata,
    output wire [19:0] address,
    output wire [7:0] wdata,
    output wire write_cycle,
    output wire [1:0] fault,
    output wire guest_ce, halted,
    output wire accepted, completed,
    output wire [1:0] cycle_kind,
    output wire [7:0] cycle_rdata,
    output wire [11:0] cga_font_address, mda_font_address,
    input wire [7:0] cga_font_data, mda_font_data,
    output wire [1:0] pixel_ce, de, hs, vs, sof, eol,
    output wire [7:0] rgbi,
    output wire dma_owner, refresh_done, audio_valid,
    output wire signed [15:0] audio_sample
);
    wire [1:0] cpu_fault, dma_fault;
    assign fault = cpu_fault | dma_fault;
    wire cycle_valid, cycle_ready, cycle_done, cycle_write, cycle_lock, bus_locked, intr;
    wire [19:0] cycle_address;
    wire [7:0] cycle_wdata;
    wire [1:0] cycle_fault;
    wire isa_valid, isa_ready, isa_done, aen, terminal_count, locked;
    wire [1:0] isa_fault, kind, dma_channel;
    wire [7:0] isa_rdata, irq;
    wire [2:0] drq;
    assign accepted = cycle_valid && cycle_ready;
    assign completed = cycle_done;
    wire cpu_clock = SEPARATE_CPU_CLOCK ? clk_cpu : clk;
    wire unused_optional_cpu_clock = SEPARATE_CPU_CLOCK ? 1'b0 : clk_cpu;
    wire cpu_reset, board_guest_ce;
    wire board_cycle_valid, board_cycle_ready, board_cycle_done;
    wire [19:0] board_cycle_address;
    wire [7:0] board_cycle_wdata, board_cycle_rdata;
    wire [1:0] board_cycle_kind, board_cycle_fault;
    wire board_cycle_write, board_cycle_lock;
    generate if (SEPARATE_CPU_CLOCK) begin: separate_clock
        // Related-clock reset transfer, then a full CPU-cycle reset fanout.
        reg reset_phase = 1'b1, reset_fast = 1'b1;
        always @(negedge clk_cpu) reset_phase <= reset;
        always @(posedge clk_cpu) reset_fast <= reset_phase;
        assign cpu_reset = reset_fast;
        pcxt_cpu_clock_bridge bridge (
            .clk_cpu(clk_cpu), .clk_board(clk), .reset_cpu(cpu_reset), .reset_board(reset),
            .cpu_valid(cycle_valid), .cpu_ready(cycle_ready), .cpu_address(cycle_address),
            .cpu_wdata(cycle_wdata), .cpu_kind(cycle_kind), .cpu_write(cycle_write), .cpu_lock(cycle_lock),
            .cpu_done(cycle_done), .cpu_rdata(cycle_rdata), .cpu_fault(cycle_fault),
            .board_valid(board_cycle_valid), .board_ready(board_cycle_ready),
            .board_address(board_cycle_address), .board_wdata(board_cycle_wdata), .board_kind(board_cycle_kind),
            .board_write(board_cycle_write), .board_lock(board_cycle_lock),
            .board_done(board_cycle_done), .board_rdata(board_cycle_rdata), .board_fault(board_cycle_fault),
            .cpu_guest_ce(guest_ce), .board_guest_ce(board_guest_ce)
        );
    end else begin: common_clock
        assign cpu_reset = reset;
        assign board_cycle_valid = cycle_valid;
        assign cycle_ready = board_cycle_ready;
        assign board_cycle_address = cycle_address;
        assign board_cycle_wdata = cycle_wdata;
        assign board_cycle_kind = cycle_kind;
        assign board_cycle_write = cycle_write;
        assign board_cycle_lock = cycle_lock;
        assign cycle_done = board_cycle_done;
        assign cycle_rdata = board_cycle_rdata;
        assign cycle_fault = board_cycle_fault;
        assign board_guest_ce = guest_ce;
    end endgenerate
    pcxt_cpu cpu (
        .clk(cpu_clock), .reset(cpu_reset), .intr(intr), .nmi(1'b0),
        .cycle_valid(cycle_valid), .cycle_ready(cycle_ready),
        .cycle_address(cycle_address), .cycle_wdata(cycle_wdata),
        .cycle_kind(cycle_kind), .cycle_write(cycle_write), .cycle_lock(cycle_lock),
        .cycle_done(cycle_done), .cycle_rdata(cycle_rdata), .cycle_fault(cycle_fault),
        .fault(cpu_fault), .guest_ce(guest_ce), .halted(halted), .bus_locked(bus_locked)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_motherboard #(.CLOCK_HZ(BOARD_CLOCK_HZ)) board (
        .clk(clk), .reset(reset), .guest_ce(board_guest_ce),
        .cycle_valid(board_cycle_valid), .cycle_ready(board_cycle_ready),
        .cycle_address(board_cycle_address), .cycle_wdata(board_cycle_wdata),
        .cycle_kind(board_cycle_kind), .cycle_write(board_cycle_write), .cycle_lock(board_cycle_lock),
        .cycle_done(board_cycle_done), .cycle_rdata(board_cycle_rdata), .cycle_fault(board_cycle_fault),
        .address(address), .wdata(wdata), .kind(kind), .write_cycle(write_cycle), .locked(locked),
        .memory_valid(memory_valid), .memory_ready(memory_ready),
        .memory_done(memory_done), .memory_rdata(memory_rdata),
        .cpu_bus_lock(bus_locked), .isa_drq(drq), .dma_fault(dma_fault),
        .dma_owner(dma_owner), .refresh_done(refresh_done), .aen(aen), .dma_channel(dma_channel),
        .terminal_count(terminal_count),
        .isa_valid(isa_valid), .isa_ready(isa_ready), .isa_done(isa_done),
        .isa_rdata(isa_rdata), .isa_fault(isa_fault), .isa_irq(irq[7:2]),
        .switches(8'h2d), .parity_error(1'b0), .channel_error(1'b0),
        .parity_enable(), .channel_check_enable(),
        .keyboard_clock(1'b1), .keyboard_data(1'b0),
        .keyboard_clock_low(), .keyboard_data_low(), .keyboard_clear(), .keyboard_irq(),
        .speaker(), .audio_valid(audio_valid), .audio_sample(audio_sample), .intr(intr), .pit_out(), .pit_clock()
    );
    pcxt_display_bus #(.DIAGNOSTIC_PROBE(DIAGNOSTIC_PROBE), .CLOCK_HZ(BOARD_CLOCK_HZ)) isa (
        .clk(clk), .reset(reset), .cycle_valid(isa_valid), .cycle_ready(isa_ready),
        .cycle_address(address), .cycle_wdata(wdata), .cycle_kind(kind),
        .cycle_write(write_cycle), .cycle_aen(aen), .cycle_lock(locked),
        .cycle_dma_channel(dma_channel), .cycle_terminal_count(terminal_count), .guest_ce(board_guest_ce),
        .cycle_done(isa_done), .cycle_rdata(isa_rdata), .cycle_fault(isa_fault), .irq(irq), .drq(drq),
        .light_pen_trigger(1'b0), .light_pen_switch(1'b0),
        .cga_font_address(cga_font_address), .mda_font_address(mda_font_address),
        .cga_font_data(cga_font_data), .mda_font_data(mda_font_data),
        .pixel_ce(pixel_ce), .de(de), .hs(hs), .vs(vs), .sof(sof), .eol(eol), .rgbi(rgbi),
        .unsupported_interlace()
    );
    /* verilator lint_on PINCONNECTEMPTY */
    wire unused_irq = ^irq[1:0];
endmodule

/* verilator lint_off DECLFILENAME */
// SPDX-License-Identifier: GPL-2.0-or-later
// One outstanding CPU100/board50 transaction on related clocks from one PLL.
// Resets must be coordinated in both domains. Negative-edge transfer registers
// give the cross-domain bundle a 5 ns setup/hold relationship; no timing cut or
// multicycle is required. CPU guest events are spaced at least 20 fast clocks.
module pcxt_cpu_clock_bridge (
    input wire clk_cpu, clk_board, reset_cpu, reset_board,
    input wire cpu_valid,
    output wire cpu_ready,
    input wire [19:0] cpu_address,
    input wire [7:0] cpu_wdata,
    input wire [1:0] cpu_kind,
    input wire cpu_write, cpu_lock,
    output reg cpu_done,
    output reg [7:0] cpu_rdata,
    output reg [1:0] cpu_fault,
    output reg board_valid,
    input wire board_ready,
    output wire [19:0] board_address,
    output wire [7:0] board_wdata,
    output wire [1:0] board_kind,
    output wire board_write, board_lock,
    input wire board_done,
    input wire [7:0] board_rdata,
    input wire [1:0] board_fault,
    input wire cpu_guest_ce,
    output reg board_guest_ce
);
    localparam [1:0] F_IDLE=0, F_WAIT=1, F_DRAIN=2;
    localparam [1:0] B_IDLE=0, B_ISSUE=1, B_WAIT=2, B_REPLY=3;
    reg [1:0] fast_state, board_state;
    reg request_active, request_phase_valid;
    reg [31:0] request_data, request_phase_data, board_packet;
    reg response_valid, response_phase_valid;
    reg [9:0] response_data, response_phase_data;
    reg guest_toggle, guest_seen;
    assign cpu_ready = fast_state == F_IDLE && !reset_cpu;
    assign {board_address,board_wdata,board_kind,board_write,board_lock} = board_packet;
    always @(posedge clk_cpu) begin
        cpu_done <= 0;
        if (reset_cpu) begin
            fast_state <= F_IDLE; request_active <= 0; request_data <= 0;
            cpu_rdata <= 8'hff; cpu_fault <= 0;
        end else case (fast_state)
            F_IDLE: if (cpu_valid) begin
                request_data <= {cpu_address,cpu_wdata,cpu_kind,cpu_write,cpu_lock};
                request_active <= 1;
                fast_state <= F_WAIT;
            end
            F_WAIT: if (response_phase_valid) begin
                {cpu_fault,cpu_rdata} <= response_phase_data;
                cpu_done <= 1;
                request_active <= 0;
                fast_state <= F_DRAIN;
            end
            F_DRAIN: if (!response_phase_valid) fast_state <= F_IDLE;
            default: begin fast_state <= F_IDLE; request_active <= 0; end
        endcase
    end
    always @(negedge clk_cpu) begin
        if (reset_cpu) begin
            request_phase_valid <= 0; request_phase_data <= 0;
            response_phase_valid <= 0; response_phase_data <= 10'h0ff;
            guest_toggle <= 0;
        end else begin
            request_phase_valid <= request_active;
            request_phase_data <= request_data;
            response_phase_valid <= response_valid;
            response_phase_data <= response_data;
            if (cpu_guest_ce) guest_toggle <= !guest_toggle;
        end
    end
    always @(posedge clk_board) begin
        if (reset_board) begin
            board_state <= B_IDLE; board_valid <= 0; board_packet <= 0;
            response_valid <= 0; response_data <= 10'h0ff;
            guest_seen <= 0; board_guest_ce <= 0;
        end else begin
            guest_seen <= guest_toggle;
            board_guest_ce <= guest_seen != guest_toggle;
            case (board_state)
                B_IDLE: if (request_phase_valid) begin
                    board_packet <= request_phase_data;
                    board_valid <= 1;
                    board_state <= B_ISSUE;
                end
                B_ISSUE: if (board_ready) begin
                    board_valid <= 0;
                    if (board_done) begin
                        response_data <= {board_fault,board_rdata};
                        response_valid <= 1;
                        board_state <= B_REPLY;
                    end else board_state <= B_WAIT;
                end
                B_WAIT: if (board_done) begin
                    response_data <= {board_fault,board_rdata};
                    response_valid <= 1;
                    board_state <= B_REPLY;
                end
                B_REPLY: if (!request_phase_valid) begin
                    response_valid <= 0;
                    board_state <= B_IDLE;
                end
                default: begin board_state <= B_IDLE; board_valid <= 0; response_valid <= 0; end
            endcase
        end
    end
endmodule

/* verilator lint_on DECLFILENAME */
