// SPDX-License-Identifier: GPL-2.0-or-later
// Byte bus router and first XT motherboard devices. All pins are synchronous
// data in the selected board clock domain. Storage and ISA each accept one request and return one done;
// reset must cancel outstanding work in both downstream participants as well.
module pcxt_motherboard #(parameter integer CLOCK_HZ = 100000000) (
    input wire clk, reset, guest_ce,
    input wire cycle_valid,
    output wire cycle_ready,
    input wire [19:0] cycle_address,
    input wire [7:0] cycle_wdata,
    input wire [1:0] cycle_kind,
    input wire cycle_write, cycle_lock, cpu_bus_lock,
    output wire cycle_done,
    output reg [7:0] cycle_rdata,
    output reg [1:0] cycle_fault,
    // Latched payload shared by the mutually exclusive downstream ports.
    output reg [19:0] address,
    output reg [7:0] wdata,
    output reg [1:0] kind,
    output reg write_cycle, locked, aen,
    output reg [1:0] dma_channel,
    output reg terminal_count,
    input wire [3:1] isa_drq,
    output wire [1:0] dma_fault,
    output wire refresh_done,
    output reg dma_owner,
    output wire memory_valid,
    input wire memory_ready, memory_done,
    input wire [7:0] memory_rdata,
    output wire isa_valid,
    input wire isa_ready, isa_done,
    input wire [7:0] isa_rdata,
    input wire [1:0] isa_fault,
    input wire [7:2] isa_irq,
    input wire [7:0] switches,
    input wire parity_error, channel_error,
    output wire parity_enable, channel_check_enable,
    input wire keyboard_clock, keyboard_data,
    output wire keyboard_clock_low, keyboard_data_low, keyboard_clear,
    output wire keyboard_irq, speaker,
    output wire audio_valid,
    output wire signed [15:0] audio_sample,
    output wire intr,
    output wire [2:0] pit_out,
    output wire pit_clock
);
    localparam IDLE=3'd0, MEMORY_SEND=3'd1, MEMORY_WAIT=3'd2,
               ISA_SEND=3'd3, ISA_WAIT=3'd4, LOCAL=3'd5, DONE=3'd6;
    reg [2:0] state;
    reg [4:0] phase;
    reg [7:0] local_read_snapshot;
    reg select_pit, select_ppi, select_dma, select_page, select_inta;
    reg source_dma, inta_pending;
    reg [1:0] pit_divider;
    wire dma_valid, dma_ready, dma_done, dma_write, dma_tc, dma_hold;
    wire [19:0] dma_address;
    wire [7:0] dma_wdata, dma_reg_data;
    wire [1:0] dma_kind, dma_ch;
    // Grant only between completed requests. Continuous CPU LOCK protects
    // instruction gaps; inta_pending protects the entire two-ack sequence.
    wire grant_dma = state == IDLE && !dma_owner && dma_hold && !cpu_bus_lock &&
                     !inta_pending && !(cycle_valid && cycle_lock);
    wire request_valid = dma_owner ? dma_valid : cycle_valid && !grant_dma;
    wire [19:0] request_address = dma_owner ? dma_address : cycle_address;
    wire [7:0] request_wdata = dma_owner ? dma_wdata : cycle_wdata;
    wire [1:0] request_kind = dma_owner ? dma_kind : cycle_kind;
    wire request_write = dma_owner ? dma_write : cycle_write;
    // RAM and ROM bounds are aligned to 64 KiB; low address bits cannot
    // affect dispatch. Keep this region decode out of a wide carry chain.
    wire ram = request_kind == 0 && request_address[19:16] < 4'ha;
    wire rom = request_kind == 0 && request_address[19:16] == 4'hf;
    // XT motherboard ignores A15:10. PIC uses A0; PIT/PPI use A1:0;
    // DMA uses A3:0 and the write-only page latch uses A1:0.
    wire pic = !dma_owner && request_kind == 1 && request_address[9:5] == 1;
    wire pit = !dma_owner && request_kind == 1 && request_address[9:5] == 2;
    wire ppi = !dma_owner && request_kind == 1 && request_address[9:5] == 3;
    wire dma = !dma_owner && request_kind == 1 && request_address[9:5] == 0;
    wire page_select = !dma_owner && request_kind == 1 && request_address[9:5] == 4;
    wire inta = !dma_owner && request_kind == 2;
    wire local_active = state == LOCAL && !reset;
    // Setup, eight active clocks, then nine release clocks. Keeping CS and
    // address stable through release lets the imported edge detectors retire
    // writes and read side effects exactly once before completion.
    reg strobe_q, pic_selected, pit_selected, ppi_selected;
    // Predict the next cycle's pins from the request/setup and release edges.
    // This preserves all eight active and nine release clocks while keeping
    // the state/phase decoder out of the imported devices' same-cycle logic.
    always @(posedge clk) begin
        if (reset) begin
            strobe_q <= 0;
            pic_selected <= 0; pit_selected <= 0; ppi_selected <= 0;
        end else begin
            strobe_q <= state == LOCAL && phase < 8;
            if (state == IDLE && request_valid) begin
                pic_selected <= pic; pit_selected <= pit; ppi_selected <= ppi;
            end else if (state == LOCAL && phase == 17) begin
                pic_selected <= 0; pit_selected <= 0; ppi_selected <= 0;
            end
        end
    end
    wire strobe = strobe_q && !reset;
    wire pic_cs_n = !(pic_selected && !reset);
    wire pit_cs_n = !(pit_selected && !reset);
    wire ppi_cs_n = !(ppi_selected && !reset);
    wire rd_n = !(strobe && !write_cycle && !select_inta);
    wire wr_n = !(strobe && write_cycle && !select_inta);
    wire [7:0] pic_data, pit_data, ppi_data;
    wire pic_bus_input, pit_gate2;

    assign cycle_ready = state == IDLE && !dma_owner && !grant_dma && !reset;
    assign dma_ready = state == IDLE && dma_owner && !reset;
    assign dma_done = state == DONE && source_dma && !reset;
    assign cycle_done = state == DONE && !source_dma && !reset;
    assign memory_valid = state == MEMORY_SEND && !reset;
    assign isa_valid = state == ISA_SEND && !reset;
    assign pit_clock = pit_divider[1];
    always @(posedge clk) begin
        if (reset) pit_divider <= 0;
        else if (guest_ce) pit_divider <= pit_divider + 1'b1;
    end

    always @(posedge clk) begin
        if (reset) begin dma_owner<=0; inta_pending<=0; end
        else begin
            if (grant_dma) dma_owner<=1;
            else if (state == IDLE && dma_owner && !dma_hold) dma_owner<=0;
            if (cycle_done && kind == 2) inta_pending<=!inta_pending;
        end
    end
    fes_dma8237 dma_device (
        .clk(clk), .reset(reset), .guest_ce(guest_ce),
        .reg_read(local_active && select_dma && phase == 8 && !write_cycle),
        .reg_write(local_active && select_dma && phase == 8 && write_cycle),
        .page_write(local_active && select_page && phase == 8 && write_cycle),
        .reg_address(address[3:0]), .reg_wdata(wdata), .reg_rdata(dma_reg_data),
        .drq(isa_drq), .refresh_clock(pit_out[1]), .hold_request(dma_hold), .granted(dma_owner),
        .cpu_waiting(cycle_valid), .cpu_done(cycle_done),
        .cycle_valid(dma_valid), .cycle_ready(dma_ready), .cycle_done(dma_done),
        .cycle_address(dma_address), .cycle_wdata(dma_wdata), .cycle_kind(dma_kind),
        .cycle_channel(dma_ch), .cycle_write(dma_write), .cycle_tc(dma_tc),
        .cycle_rdata(cycle_rdata), .cycle_fault(cycle_fault), .fault(dma_fault), .refresh_done(refresh_done)
    );
    always @(posedge clk) begin
        if (reset) begin
            source_dma<=0; aen<=0; dma_channel<=0; terminal_count<=0;
            state <= IDLE;
            phase <= 0;
            select_pit <= 0; select_ppi <= 0; select_dma <= 0; select_page <= 0; select_inta <= 0;
            address <= 0; wdata <= 0; kind <= 0;
            write_cycle <= 0; locked <= 0;
            cycle_rdata <= 8'hff; cycle_fault <= 0;
        end else case (state)
            IDLE: if (request_valid) begin
                source_dma<=dma_owner; aen<=dma_owner;
                dma_channel<=dma_owner ? dma_ch : 2'b0; terminal_count<=dma_owner && dma_tc;
                address <= request_address; wdata <= request_wdata;
                kind <= request_kind; write_cycle <= request_write;
                locked <= !dma_owner && cycle_lock;
                select_pit <= pit; select_ppi <= ppi; select_dma <= dma; select_page <= page_select; select_inta <= inta;
                phase <= 0; cycle_rdata <= 8'hff; cycle_fault <= 0;
                if (rom && request_write) state <= DONE;
                else if (ram || rom) state <= MEMORY_SEND;
                else if (pic || pit || ppi || dma || page_select || inta) state <= LOCAL;
                else state <= ISA_SEND;
            end
            MEMORY_SEND: if (memory_ready) begin
                if (memory_done) begin
                    cycle_rdata <= memory_rdata; state <= DONE;
                end else state <= MEMORY_WAIT;
            end
            MEMORY_WAIT: if (memory_done) begin
                cycle_rdata <= memory_rdata; state <= DONE;
            end
            ISA_SEND: if (isa_ready) begin
                if (isa_done) begin
                    cycle_rdata <= isa_rdata; cycle_fault <= isa_fault; state <= DONE;
                end else state <= ISA_WAIT;
            end
            ISA_WAIT: if (isa_done) begin
                cycle_rdata <= isa_rdata; cycle_fault <= isa_fault; state <= DONE;
            end
            LOCAL: begin
                phase <= phase + 1'b1;
                if (phase == 17) begin
                    if (!write_cycle) cycle_rdata <= local_read_snapshot;
                    state <= DONE;
                end
            end
            default: state <= IDLE;
        endcase
    end

    // Sample at the original read edge. The existing release interval lets
    // this local mux settle before the shared memory/ISA return register.
    always @(posedge clk) begin
        if (reset) local_read_snapshot <= 8'hff;
        else if (state == LOCAL && phase == 8 && !write_cycle) begin
            if (select_dma) local_read_snapshot <= dma_reg_data;
            else if (select_page) local_read_snapshot <= 8'hff;
            else if (select_ppi) local_read_snapshot <= ppi_data;
            else if (select_pit) local_read_snapshot <= pit_data;
            else local_read_snapshot <= pic_bus_input ? 8'hff : pic_data;
        end
    end

    pcxt_xtio xtio (
        .clk(clk), .reset(reset), .chip_select_n(ppi_cs_n),
        .read_enable_n(rd_n), .write_enable_n(wr_n), .address(address[1:0]),
        .wdata(wdata), .rdata(ppi_data), .switches(switches),
        .parity_error(parity_error), .channel_error(channel_error),
        .parity_enable(parity_enable), .channel_check_enable(channel_check_enable),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data),
        .keyboard_clock_low(keyboard_clock_low), .keyboard_data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear),
        .keyboard_irq(keyboard_irq), .pit_out2(pit_out[2]),
        .pit_gate2(pit_gate2), .speaker(speaker)
    );
    pcxt_speaker #(.CLOCK_HZ(CLOCK_HZ)) audio (
        .clk(clk), .reset(reset), .speaker(speaker),
        .sample_valid(audio_valid), .sample(audio_sample)
    );

    /* verilator lint_off PINCONNECTEMPTY */
    KF8259 pic_device (
        .clock(clk), .reset(reset),
        .chip_select_n(pic_cs_n),
        .read_enable_n(rd_n), .write_enable_n(wr_n), .address(address[0]),
        .data_bus_in(wdata), .data_bus_out(pic_data), .data_bus_io(pic_bus_input),
        .cascade_in(3'b0), .cascade_out(), .cascade_io(), .slave_program_n(1'b1),
        .buffer_enable(), .slave_program_or_enable_buffer(),
        .interrupt_acknowledge_n(!(strobe && select_inta)),
        .interrupt_to_cpu(intr), .interrupt_request({isa_irq, keyboard_irq, pit_out[0]})
    );
    /* verilator lint_on PINCONNECTEMPTY */
    KF8253 pit_device (
        .clock(clk), .reset(reset),
        .chip_select_n(pit_cs_n),
        .read_enable_n(rd_n), .write_enable_n(wr_n), .address(address[1:0]),
        .data_bus_in(wdata), .data_bus_out(pit_data),
        .counter_0_clock(pit_clock), .counter_0_gate(1'b1), .counter_0_out(pit_out[0]),
        .counter_1_clock(pit_clock), .counter_1_gate(1'b1), .counter_1_out(pit_out[1]),
        .counter_2_clock(pit_clock), .counter_2_gate(pit_gate2), .counter_2_out(pit_out[2])
    );
endmodule
