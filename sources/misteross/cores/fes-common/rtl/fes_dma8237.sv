// SPDX-License-Identifier: GPL-2.0-or-later
// Transaction-native 8237A subset shared by the PC/XT and pc386. A peripheral
// byte and its memory half retire before address/count change. No pin-level
// timing equivalence.
//
// The defaults are the XT: 20-bit addresses from four 4-bit page latches
// indexed as the XT's 81h-83h, and channel 0 serving DRAM refresh from the
// PIT. The AT's first controller sets ADDRESS_BITS=24, AT_PAGES=1 (one page
// per channel, written with page_write at the channel's index) and REFRESH=0
// (channel 0 then has no request source). Its second controller adds
// WORD_ADDRESS=1: addresses and counts are words, so the cycle address is the
// page without bit 0 followed by the word address shifted left once.
module fes_dma8237 #(
    parameter integer ADDRESS_BITS = 20,
    parameter integer AT_PAGES = 0,
    parameter integer REFRESH = 1,
    parameter integer WORD_ADDRESS = 0
) (
    input wire clk, reset, guest_ce,
    input wire reg_read, reg_write, page_write,
    input wire [3:0] reg_address,
    input wire [7:0] reg_wdata,
    output reg [7:0] reg_rdata,
    input wire [3:1] drq,
    input wire refresh_clock,
    output wire hold_request,
    input wire granted, cpu_waiting, cpu_done,
    output wire cycle_valid,
    input wire cycle_ready, cycle_done,
    output wire [ADDRESS_BITS-1:0] cycle_address,
    output wire [7:0] cycle_wdata,
    output wire [1:0] cycle_kind, cycle_channel,
    output wire cycle_write, cycle_tc,
    input wire [7:0] cycle_rdata,
    input wire [1:0] cycle_fault,
    output reg [1:0] fault,
    output reg refresh_done
);
    localparam integer PAGE_BITS = ADDRESS_BITS - 16;
    localparam IDLE=4'd0, SELECT=4'd1, DEV_SEND=4'd2, DEV_WAIT=4'd3,
               MEM_SEND=4'd4, MEM_WAIT=4'd5, COMPLETE=4'd6, YIELD=4'd7, FAILED=4'd8;
    reg [3:0] state;
    reg [15:0] base_address[0:3], current_address[0:3];
    reg [15:0] base_count[0:3], current_count[0:3];
    reg [7:0] mode[0:3];
    reg [PAGE_BITS-1:0] page[0:3];
    reg [7:0] command;
    reg [3:0] mask_bits, software_request, tc_status;
    reg byte_high;
    reg [1:0] channel, lowest;
    reg [7:0] transfer_data;
    reg [ADDRESS_BITS-1:0] transfer_address;
    reg terminal, service_started;
    reg [1:0] minimum_wait;
    reg refresh_pending, refresh_previous, yield_served;
    wire refresh_edge = refresh_clock && !refresh_previous;
    // refresh_pending is the XT DREQ0 pin: a 74LS74 Q set by the PIT1 rising
    // edge and cleared when service starts. Command bit 6 is the 8237 sense
    // of every DREQ pin, so an idle low latch is a request when sense is low.
    wire [3:0] hardware_request = {drq, refresh_pending} ^ {4{command[6]}};
    wire [3:0] requests = hardware_request | software_request;
    // Intel specifies software requests only in block mode. Keep their
    // status bits visible, but do not invent service in the other modes.
    wire [3:0] block_mode = {mode[3][7:6] == 2, mode[2][7:6] == 2,
                             mode[1][7:6] == 2, mode[0][7:6] == 2};
    wire [3:0] eligible = (hardware_request | (software_request & block_mode)) & ~mask_bits;
    wire chosen_valid = |eligible;
    wire [3:1] winners;
    wire [1:0] chosen = {winners[2] | winners[3], winners[1] | winners[3]};
    integer n;
    // Evaluate all circular priority masks in parallel. lowest is the last
    // serviced channel; rotating priority starts immediately after it.
    wire [3:0] higher1 = !command[4] ? 4'h1 :
        lowest == 0 ? 4'h0 : lowest == 1 ? 4'hd :
        lowest == 2 ? 4'h9 : 4'h1;
    assign winners[1] = eligible[1] && !(|(eligible & higher1));
    wire [3:0] higher2 = !command[4] ? 4'h3 :
        lowest == 0 ? 4'h2 : lowest == 1 ? 4'h0 :
        lowest == 2 ? 4'hb : 4'h3;
    assign winners[2] = eligible[2] && !(|(eligible & higher2));
    wire [3:0] higher3 = !command[4] ? 4'h7 :
        lowest == 0 ? 4'h6 : lowest == 1 ? 4'h4 :
        lowest == 2 ? 4'h0 : 4'h7;
    assign winners[3] = eligible[3] && !(|(eligible & higher3));
    // Resolve a new service at grant, not when HRQ first becomes pending:
    // a higher-priority request can arrive during a long CPU wait or LOCK.
    wire [1:0] service_channel = state == SELECT && !service_started ? chosen : channel;
    wire [1:0] operation = mode[service_channel][3:2];
    wire [1:0] transfer_mode = mode[service_channel][7:6];
    wire memory_phase = state == MEM_SEND || state == MEM_WAIT;
    // The XT has latches at 81h (channel 2), 82h (3) and 83h (1, shared by 0).
    wire [PAGE_BITS-1:0] service_page = AT_PAGES != 0 ? page[service_channel] :
        page[service_channel == 2 ? 2'd1 : service_channel == 3 ? 2'd2 : 2'd3];
    assign hold_request = state != IDLE && state != YIELD && state != FAILED && !reset;
    assign cycle_valid = !reset && granted && (state == MEM_SEND || state == DEV_SEND);
    assign cycle_address = memory_phase ? transfer_address : {ADDRESS_BITS{1'b0}};
    assign cycle_wdata = transfer_data;
    assign cycle_kind = memory_phase ? 2'd0 : 2'd3;
    assign cycle_write = memory_phase ? operation == 1 : operation == 2;
    assign cycle_channel = memory_phase ? 2'b0 : channel;
    assign cycle_tc = !memory_phase && terminal;
    always @* begin
        reg_rdata = 8'hff;
        if (!reg_address[3]) begin
            if (reg_address[0])
                reg_rdata = byte_high ? current_count[reg_address[2:1]][15:8] : current_count[reg_address[2:1]][7:0];
            else
                reg_rdata = byte_high ? current_address[reg_address[2:1]][15:8] : current_address[reg_address[2:1]][7:0];
        end else if (reg_address == 8) reg_rdata = {requests, tc_status};
        // Temporary register is used only by unsupported memory-to-memory.
        else if (reg_address == 13) reg_rdata = 0;
    end
    always @(posedge clk) begin
        if (reset) begin
            state <= IDLE; command <= 0; mask_bits <= 15; software_request <= 0;
            tc_status <= 0; byte_high <= 0; channel <= 0; lowest <= 3;
            transfer_data <= 0; transfer_address <= 0; terminal <= 0; fault <= 0; minimum_wait<=0; service_started<=0;
            refresh_pending <= 0; refresh_previous <= 0; refresh_done <= 0; yield_served <= 0;
            for (n=0;n<4;n=n+1) begin
                base_address[n]<=0; current_address[n]<=0; base_count[n]<=0; current_count[n]<=0;
                mode[n]<=0; page[n]<=0;
            end
        end else begin
            refresh_previous <= refresh_clock; refresh_done <= 0;
            if (guest_ce && minimum_wait != 0) minimum_wait<=minimum_wait-1'b1;
            if (REFRESH != 0 && refresh_edge) refresh_pending <= 1;
            if (page_write) page[reg_address[1:0]] <= reg_wdata[PAGE_BITS-1:0];
            if (reg_read) begin
                if (!reg_address[3]) byte_high <= !byte_high;
                if (reg_address == 8) tc_status <= 0;
            end
            if (reg_write) begin
                if (!reg_address[3]) begin
                    if (reg_address[0]) begin
                        if (byte_high) begin
                            base_count[reg_address[2:1]][15:8] <= reg_wdata;
                            current_count[reg_address[2:1]][15:8] <= reg_wdata;
                        end else begin
                            base_count[reg_address[2:1]][7:0] <= reg_wdata;
                            current_count[reg_address[2:1]][7:0] <= reg_wdata;
                        end
                    end else begin
                        if (byte_high) begin
                            base_address[reg_address[2:1]][15:8] <= reg_wdata;
                            current_address[reg_address[2:1]][15:8] <= reg_wdata;
                        end else begin
                            base_address[reg_address[2:1]][7:0] <= reg_wdata;
                            current_address[reg_address[2:1]][7:0] <= reg_wdata;
                        end
                    end
                    byte_high <= !byte_high;
                end else case (reg_address)
                    8: command <= reg_wdata;
                    9: software_request[reg_wdata[1:0]] <= reg_wdata[2];
                    10: mask_bits[reg_wdata[1:0]] <= reg_wdata[2];
                    11: mode[reg_wdata[1:0]] <= reg_wdata;
                    12: byte_high <= 0;
                    14: mask_bits <= 0;
                    15: mask_bits <= reg_wdata[3:0];
                    default: begin end
                endcase
            end
            // Master clear preserves address/count/mode/page programming.
            if (reg_write && reg_address == 13) begin
                state<=IDLE; command<=0; mask_bits<=15; software_request<=0;
                tc_status<=0; byte_high<=0; lowest<=3; fault<=0;
                transfer_data<=0;
            end else case (state)
                IDLE: if (guest_ce && !command[2] && chosen_valid && !reg_write) begin
                    channel<=chosen; state<=SELECT; service_started<=0;
                end
                SELECT: if (granted && guest_ce) begin
                    // Unsupported bus modes fail visibly instead of producing
                    // misleading device commits. External EOP is not exposed.
                    if (command[2] || (!service_started && !chosen_valid) ||
                        (service_started && (mask_bits[channel] ||
                         (transfer_mode == 0 && !hardware_request[channel])))) begin
                        yield_served<=0; state<=YIELD;
                    end else if (command[0] || transfer_mode == 3 || operation == 3 ||
                        (service_channel == 0 && operation == 1)) begin fault<=3; state<=FAILED; end
                    else begin
                        channel<=service_channel; service_started<=1; minimum_wait<=3;
                        if (service_channel == 0) refresh_pending<=REFRESH != 0 && refresh_edge;
                        transfer_address <= WORD_ADDRESS != 0 ?
                            {service_page[PAGE_BITS-1:1], current_address[service_channel], 1'b0} :
                            {service_page, current_address[service_channel]};
                        terminal <= current_count[service_channel] == 0;
                        if (operation == 0) state<=COMPLETE;
                        else if (operation == 1) state<=DEV_SEND;
                        else state<=MEM_SEND;
                    end
                end
                DEV_SEND: if (cycle_ready) state<=DEV_WAIT;
                MEM_SEND: if (cycle_ready) state<=MEM_WAIT;
                DEV_WAIT: if (cycle_done) begin
                    if (cycle_fault != 0) begin fault<=cycle_fault; state<=FAILED; end
                    else if (operation == 1) begin transfer_data<=cycle_rdata; state<=MEM_SEND; end
                    else state<=COMPLETE;
                end
                MEM_WAIT: if (cycle_done) begin
                    if (cycle_fault != 0) begin fault<=cycle_fault; state<=FAILED; end
                    else if (operation == 2 && channel != 0) begin transfer_data<=cycle_rdata; state<=DEV_SEND; end
                    else state<=COMPLETE;
                end
                COMPLETE: if (guest_ce && minimum_wait == 0) begin
                    current_address[channel] <= current_address[channel] + (mode[channel][5] ? 16'hffff : 16'd1);
                    current_count[channel] <= current_count[channel] - 1'b1;
                    if (REFRESH != 0 && channel == 0) refresh_done<=1;
                    if (command[4]) lowest<=channel;
                    if (terminal) begin
                        tc_status[channel]<=1; software_request[channel]<=0;
                        if (mode[channel][4]) begin
                            current_address[channel]<=base_address[channel]; current_count[channel]<=base_count[channel];
                        end else mask_bits[channel]<=1;
                    end
                    yield_served<=0;
                    if (!terminal && (transfer_mode == 2 || (transfer_mode == 0 && hardware_request[channel]))) state<=SELECT;
                    else state<=YIELD;
                end
                YIELD: begin
                    if (cpu_done) yield_served<=1;
                    if (guest_ce && (!cpu_waiting || yield_served)) state<=IDLE;
                end
                default: begin end
            endcase
        end
    end
    wire unused_command = ^{command[7:5], command[3:1]};
    wire unused_mode = ^{mode[0][1:0],mode[1][1:0],mode[2][1:0],mode[3][1:0]};
endmodule
