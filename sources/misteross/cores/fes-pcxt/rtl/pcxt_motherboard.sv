// SPDX-License-Identifier: GPL-2.0-or-later
// Byte bus router and first XT motherboard devices. All pins are synchronous
// data at 100 MHz. Storage and ISA each accept one request and return one done;
// reset must cancel outstanding work in both downstream participants as well.
module pcxt_motherboard (
    input wire clk, reset, guest_ce,
    input wire cycle_valid,
    output wire cycle_ready,
    input wire [19:0] cycle_address,
    input wire [7:0] cycle_wdata,
    input wire [1:0] cycle_kind,
    input wire cycle_write, cycle_lock,
    output wire cycle_done,
    output reg [7:0] cycle_rdata,
    output reg [1:0] cycle_fault,
    // Latched payload shared by the mutually exclusive downstream ports.
    output reg [19:0] address,
    output reg [7:0] wdata,
    output reg [1:0] kind,
    output reg write_cycle, locked,
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
    output wire keyboard_clock_low, keyboard_data_low,
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
    reg select_pic, select_pit, select_ppi, select_inta;
    reg [1:0] pit_divider;
    wire ram = cycle_kind == 0 && cycle_address < 20'ha0000;
    wire rom = cycle_kind == 0 && cycle_address >= 20'hf0000;
    // XT motherboard ignores A15:10. PIC uses A0; PIT/PPI use A1:0.
    wire pic = cycle_kind == 1 && cycle_address[9:5] == 5'b00001;
    wire pit = cycle_kind == 1 && cycle_address[9:5] == 5'b00010;
    wire ppi = cycle_kind == 1 && cycle_address[9:5] == 5'b00011;
    wire inta = cycle_kind == 2;
    wire local_active = state == LOCAL && !reset;
    // Setup, eight active clocks, then nine release clocks. Keeping CS and
    // address stable through release lets the imported edge detectors retire
    // writes and read side effects exactly once before completion.
    wire strobe = local_active && phase >= 1 && phase <= 8;
    wire rd_n = !(strobe && !write_cycle && !select_inta);
    wire wr_n = !(strobe && write_cycle && !select_inta);
    wire [7:0] pic_data, pit_data, ppi_data;
    wire pic_bus_input, pit_gate2;

    assign cycle_ready = state == IDLE && !reset;
    assign cycle_done = state == DONE && !reset;
    assign memory_valid = state == MEMORY_SEND && !reset;
    assign isa_valid = state == ISA_SEND && !reset;
    assign pit_clock = pit_divider[1];
    always @(posedge clk) begin
        if (reset) pit_divider <= 0;
        else if (guest_ce) pit_divider <= pit_divider + 1'b1;
    end

    always @(posedge clk) begin
        if (reset) begin
            state <= IDLE;
            phase <= 0;
            select_pic <= 0; select_pit <= 0; select_ppi <= 0; select_inta <= 0;
            address <= 0; wdata <= 0; kind <= 0;
            write_cycle <= 0; locked <= 0;
            cycle_rdata <= 8'hff; cycle_fault <= 0;
        end else case (state)
            IDLE: if (cycle_valid) begin
                address <= cycle_address; wdata <= cycle_wdata;
                kind <= cycle_kind; write_cycle <= cycle_write;
                locked <= cycle_lock;
                select_pic <= pic; select_pit <= pit; select_ppi <= ppi; select_inta <= inta;
                phase <= 0; cycle_rdata <= 8'hff; cycle_fault <= 0;
                if (rom && cycle_write) state <= DONE;
                else if (ram || rom) state <= MEMORY_SEND;
                else if (pic || pit || ppi || inta) state <= LOCAL;
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
                if (phase == 8 && !write_cycle) begin
                    if (select_ppi) cycle_rdata <= ppi_data;
                    else if (select_pit) cycle_rdata <= pit_data;
                    else cycle_rdata <= pic_bus_input ? 8'hff : pic_data;
                end
                if (phase == 17) state <= DONE;
            end
            default: state <= IDLE;
        endcase
    end

    pcxt_xtio xtio (
        .clk(clk), .reset(reset), .chip_select_n(!(local_active && select_ppi)),
        .read_enable_n(rd_n), .write_enable_n(wr_n), .address(address[1:0]),
        .wdata(wdata), .rdata(ppi_data), .switches(switches),
        .parity_error(parity_error), .channel_error(channel_error),
        .parity_enable(parity_enable), .channel_check_enable(channel_check_enable),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data),
        .keyboard_clock_low(keyboard_clock_low), .keyboard_data_low(keyboard_data_low),
        .keyboard_irq(keyboard_irq), .pit_out2(pit_out[2]),
        .pit_gate2(pit_gate2), .speaker(speaker)
    );
    pcxt_speaker audio (
        .clk(clk), .reset(reset), .speaker(speaker),
        .sample_valid(audio_valid), .sample(audio_sample)
    );

    /* verilator lint_off PINCONNECTEMPTY */
    KF8259 pic_device (
        .clock(clk), .reset(reset),
        .chip_select_n(!(local_active && select_pic)),
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
        .chip_select_n(!(local_active && select_pit)),
        .read_enable_n(rd_n), .write_enable_n(wr_n), .address(address[1:0]),
        .data_bus_in(wdata), .data_bus_out(pit_data),
        .counter_0_clock(pit_clock), .counter_0_gate(1'b1), .counter_0_out(pit_out[0]),
        .counter_1_clock(pit_clock), .counter_1_gate(1'b1), .counter_1_out(pit_out[1]),
        .counter_2_clock(pit_clock), .counter_2_gate(pit_gate2), .counter_2_out(pit_out[2])
    );
endmodule
