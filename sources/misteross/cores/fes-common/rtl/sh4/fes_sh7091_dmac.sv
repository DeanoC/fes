// SH7091 DMA. Each transfer unit is read into a buffer before
// writes begin, so a wide unit can travel over the 32-bit external port.
module fes_sh7091_dmac (
    input  logic        clk, reset, nmi, pause, stopped,
    output logic        idle,
    input  logic        reg_request,
    input  logic [31:2] reg_addr,
    input  logic [3:0]  reg_rmask, reg_wstrb,
    input  logic [31:0] reg_wdata,
    output logic        reg_selected, reg_rvalid,
    output logic [31:0] reg_rdata,
    input  logic [1:0]  dreq_n,
    output logic [1:0]  dreq_ack,
    input  logic        capture_pending,
    input  logic [31:0] capture_value,
    output logic        capture_accept,
    output logic [4:0]  irq,
    input  logic [3:0]  serial_request,
    input  logic        ddt_valid,
    input  logic [1:0]  ddt_kind,
    input  logic [63:0] ddt_command,
    output logic        ddt_ready,
    output logic        device_valid, device_write, device_last,
    output logic [1:0]  device_channel,
    output logic [3:0]  device_mask,
    output logic [31:0] device_wdata,
    input  logic        device_ready,
    input  logic [31:0] device_rdata,
    output logic        mem_valid, mem_burst,
    output logic [1:0]  mem_serial,
    input  logic        mem_ready,
    output logic [31:0] mem_addr, mem_wdata,
    output logic [3:0]  mem_rmask, mem_wstrb,
    input  logic        mem_rvalid,
    input  logic [31:0] mem_rdata
);
    typedef enum logic [3:0] {IDLE, CAPTURE, CHECK, READ_REQUEST, READ_REPLY, WRITE_REQUEST, DEVICE_READ, DEVICE_WRITE, RETIRE} state_t;
    state_t state;
    logic [31:0] sar [0:3], dar [0:3], chcr [0:3];
    logic [23:0] count [0:3];
    logic [3:0] end_seen;
    logic [1:0] error_seen;
    logic [15:0] dmaor;
    logic [1:0] dreq_meta, dreq_sync, dreq_last, edge_pending;
    logic prior_nmi;
    logic [1:0] reg_channel, round_robin, chosen, channel;
    logic [1:0] priority_start, priority_second, priority_third, priority_last;
    logic [3:0] eligible;
    logic have_request, write_reg, read_reg, op_register, channel_write;
    logic [31:0] source, destination;
    logic [31:0] buffer [0:7];
    logic [3:0] resource_mode;
    logic [2:0] size_code, beat, last_beat;
    logic [5:0] unit_bytes;
    logic [1:0] source_mode, dest_mode;
    logic transfer_burst, captured_source, serial_tx, serial_rx, address_error;
    logic [4:0] alignment_mask;
    logic [3:0] source_mask, dest_mask;
    logic [1:0] source_lane, dest_lane;
    logic [31:0] next_source, next_destination;
    logic [28:0] serial_tx_addr, serial_rx_addr;
    logic [2:0] ddt_queued [1:3];
    logic [1:0] ddt_channel, ddt_prior_channel, ddt_mode;
    logic [2:0] dtr_size, dtr_chcr_size;
    logic ddt_prior_valid, ddt_pending, ddt_accept, ddt_setup, ddt_end, ddt_reg_command;
    logic ddt_device_to_mem, single_read, single_write, stop_unit, start_unit;
    logic [3:0] external_request, external_enabled;
    logic [15:0] unused_dtr_reserved;

    // Channel registers and DMAOR are longword P4 accesses.
    assign idle = state == IDLE;
    assign reg_channel = reg_addr[5:4];
    assign op_register = reg_addr[6];
    assign reg_selected = reg_addr[31:7] == 25'h1ff4000 && (!op_register || reg_addr[5:2] == 0);
    assign write_reg = reg_request && reg_selected && !stopped && reg_wstrb == 4'hf;
    assign read_reg = reg_request && reg_selected && !stopped && reg_wstrb == 0 && reg_rmask == 4'hf;
    assign channel_write = write_reg && !op_register && (reg_channel != 0 || !dmaor[15]);

    // The board converts the external bus handshake to command, repeat and
    // direct-channel-2 requests. The data port carries one packed 32-bit beat.
    localparam logic [1:0] DTR = 0, REPEAT = 1, DIRECT = 2;
    assign ddt_channel = ddt_kind == DIRECT ? 2'd2 :
                         ddt_kind == REPEAT ? ddt_prior_channel : ddt_command[59:58];
    assign unused_dtr_reserved = ddt_command[47:32];
    assign ddt_mode = ddt_command[57:56];
    assign dtr_size = ddt_command[63:61];
    assign dtr_chcr_size = dtr_size == 3 ? 3'd0 : dtr_size == 4 ? 3'd4 : dtr_size + 3'd1;
    assign ddt_device_to_mem = ddt_command[60];
    assign ddt_reg_command = ddt_kind == DTR && ddt_channel == 0 && ddt_mode != 0;
    assign ddt_setup = ddt_accept && ddt_reg_command && dtr_size < 5;
    assign ddt_end = ddt_accept && ddt_reg_command && dtr_size == 7;
    assign ddt_ready = dmaor[15] && !stopped && !write_reg &&
                       (ddt_kind inside {DTR, DIRECT} || (ddt_kind == REPEAT && ddt_prior_valid)) &&
                       (ddt_channel == 0 ? !ddt_pending && !(channel == 0 && !idle) &&
                        (ddt_reg_command || (chcr[0][0] && !chcr[0][1])) :
                        ddt_queued[ddt_channel] < 4);
    assign ddt_accept = ddt_valid && ddt_ready;

    for (genvar i = 0; i < 4; i++) begin : request_channel
        logic pin_request, ddt_request;
        if (i < 2) begin : has_external_pin
            assign pin_request = chcr[i][19] ? edge_pending[i] : !dreq_sync[i];
        end else begin : internal_requests_only
            assign pin_request = 0;
        end
        if (i == 0) begin : demand
            assign ddt_request = ddt_pending;
        end else begin : queued
            assign ddt_request = ddt_queued[i] != 0;
            logic enqueue, dequeue;
            assign enqueue = ddt_accept && ddt_channel == 2'(i);
            assign dequeue = dmaor[15] && start_unit && channel == 2'(i) && external_enabled[i];
            always_ff @(posedge clk)
                if (reset || !dmaor[15])
                    ddt_queued[i] <= 0;
                else if (enqueue != dequeue)
                    ddt_queued[i] <= enqueue ? ddt_queued[i] + 3'd1 : ddt_queued[i] - 3'd1;
        end
        assign external_request[i] = dmaor[15] ? ddt_request : pin_request;
        assign external_enabled[i] = chcr[i][11:8] inside {4'd0, 4'd2, 4'd3};
        assign eligible[i] = chcr[i][0] && !chcr[i][1] &&
                             ((external_enabled[i] && external_request[i]) ||
                              (i != 0 || !dmaor[15]) &&
                              (chcr[i][11:8] == 4 ||
                               (chcr[i][11:10] == 2'b10 && serial_request[chcr[i][9:8]]) ||
                               ((chcr[i][11:8] inside {4'd12, 4'd14}) && capture_pending)));
        assign irq[i] = chcr[i][2] && chcr[i][1];
    end
    assign irq[4] = dmaor[2];
    assign have_request = dmaor[0] && dmaor[2:1] == 0 && (|eligible);

    always_comb begin
        case (dmaor[9:8])
            0: {priority_start, priority_second, priority_third, priority_last} = 8'b00_01_10_11;
            1: {priority_start, priority_second, priority_third, priority_last} = 8'b00_10_11_01;
            2: {priority_start, priority_second, priority_third, priority_last} = 8'b10_00_01_11;
            default: begin
                priority_start = round_robin;
                priority_second = round_robin + 2'd1;
                priority_third = round_robin + 2'd2;
                priority_last = round_robin + 2'd3;
            end
        endcase
        chosen = eligible[priority_start] ? priority_start :
                 eligible[priority_second] ? priority_second :
                 eligible[priority_third] ? priority_third : priority_last;

        case (size_code)
            0: begin unit_bytes = 8; last_beat = 1; end
            1: begin unit_bytes = 1; last_beat = 0; end
            2: begin unit_bytes = 2; last_beat = 0; end
            3: begin unit_bytes = 4; last_beat = 0; end
            default: begin unit_bytes = 32; last_beat = 7; end
        endcase
    end

    assign alignment_mask = unit_bytes[4:0] - 5'd1;
    assign captured_source = resource_mode == 14;
    assign serial_tx = resource_mode[3:2] == 2'b10 && !resource_mode[0];
    assign serial_rx = resource_mode[3:2] == 2'b10 && resource_mode[0];
    assign serial_tx_addr = resource_mode[1] ? 29'h1fe8_000c : 29'h1fe0_000c;
    assign serial_rx_addr = resource_mode[1] ? 29'h1fe8_0014 : 29'h1fe0_0014;
    assign single_read = resource_mode == 2;
    assign single_write = resource_mode == 3;
    assign address_error = size_code > 4 ||
                           (!single_write && (source_mode == 3 || |(source[4:0] & alignment_mask) ||
                            (serial_rx ? source[28:0] != serial_rx_addr || size_code != 1 :
                             captured_source ? source != 32'hffd8_002c || size_code != 3 :
                                               source[28:26] == 3'b111))) ||
                           (!single_read && (dest_mode == 3 || |(destination[4:0] & alignment_mask) ||
                            (serial_tx ? destination[28:0] != serial_tx_addr || size_code != 1 :
                                         destination[28:26] == 3'b111)));
    assign stop_unit = pause || !dmaor[0] || dmaor[2:1] != 0 || !chcr[channel][0] || (nmi && !prior_nmi);
    assign start_unit = state == CHECK && !stop_unit && !address_error;
    assign device_valid = state inside {DEVICE_READ, DEVICE_WRITE};
    assign device_write = state == DEVICE_WRITE;
    assign device_last = beat == last_beat;
    assign device_channel = channel;
    assign device_mask = size_code == 1 ? 4'b0001 : size_code == 2 ? 4'b0011 : 4'b1111;
    assign device_wdata = buffer[beat];
    assign source_lane = source[1:0];
    assign dest_lane = destination[1:0];
    assign source_mask = size_code == 1 ? (4'b0001 << source_lane) :
                         size_code == 2 ? (4'b0011 << source_lane) : 4'hf;
    assign dest_mask = size_code == 1 ? (4'b0001 << dest_lane) :
                       size_code == 2 ? (4'b0011 << dest_lane) : 4'hf;
    assign next_source = source + {26'b0, unit_bytes};
    assign next_destination = destination + {26'b0, unit_bytes};

    assign mem_valid = state inside {READ_REQUEST, WRITE_REQUEST};
    assign mem_serial = (state == READ_REQUEST && serial_rx) || (state == WRITE_REQUEST && serial_tx) ?
                        (resource_mode[1] ? 2'b10 : 2'b01) : 2'b00;
    assign mem_burst = transfer_burst && !serial_tx && !serial_rx && state inside {READ_REQUEST, READ_REPLY, WRITE_REQUEST};
    assign mem_addr = {3'b0, (state == WRITE_REQUEST ? destination[28:2] : source[28:2]) + {24'b0, beat}, 2'b00};
    assign mem_wstrb = state == WRITE_REQUEST ? dest_mask : 4'b0;
    assign mem_rmask = source_mask;
    assign mem_wdata = buffer[beat] << {dest_lane, 3'b0};

    always_ff @(posedge clk) begin
        if (reset) begin
            state <= IDLE;
            dmaor <= 0;
            round_robin <= 0;
            end_seen <= 0;
            error_seen <= 0;
            dreq_meta <= '1;
            dreq_sync <= '1;
            dreq_last <= '1;
            edge_pending <= 0;
            prior_nmi <= 0;
            ddt_pending <= 0;
            ddt_prior_channel <= 0;
            ddt_prior_valid <= 0;
            dreq_ack <= 0;
            capture_accept <= 0;
            reg_rvalid <= 0;
            for (int i = 0; i < 4; i++)
                chcr[i] <= 0;
        end else begin
            reg_rvalid <= reg_request && reg_selected && reg_wstrb == 0;
            if (reg_request && reg_selected && reg_wstrb == 0) begin
                reg_rdata <= 0;
                if (read_reg) begin
                    if (op_register) begin
                        reg_rdata <= {16'b0, dmaor};
                        error_seen <= dmaor[2:1];
                    end else case (reg_addr[3:2])
                        0: reg_rdata <= sar[reg_channel];
                        1: reg_rdata <= dar[reg_channel];
                        2: reg_rdata <= {8'b0, count[reg_channel]};
                        3: begin
                            reg_rdata <= chcr[reg_channel];
                            end_seen[reg_channel] <= chcr[reg_channel][1];
                        end
                    endcase
                end
            end
            if (ddt_setup) begin
                if (ddt_device_to_mem)
                    dar[0] <= ddt_command[31:0];
                else
                    sar[0] <= ddt_command[31:0];
                count[0] <= {16'b0, ddt_command[55:48]};
                chcr[0] <= {12'b0, ddt_mode == 1, 3'b0, 4'b0101, 3'b001, ddt_device_to_mem,
                            ddt_mode != 3, dtr_chcr_size, 4'b0001};
                end_seen[0] <= 0;
            end else if (ddt_end) begin
                chcr[0][0] <= 0;
            end else if (channel_write) case (reg_addr[3:2])
                0: sar[reg_channel] <= reg_wdata;
                1: dar[reg_channel] <= reg_wdata;
                2: count[reg_channel] <= reg_wdata[23:0];
                3: begin
                    chcr[reg_channel] <= (reg_wdata & (reg_channel[1] ? 32'hff02_fff5 : 32'hff0f_fff5)) |
                                         {30'b0, chcr[reg_channel][1] && (reg_wdata[1] || !end_seen[reg_channel]), 1'b0};
                    end_seen[reg_channel] <= 0;
                end
            endcase
            if (write_reg && op_register) begin
                dmaor <= (reg_wdata[15:0] & 16'h8301) |
                         {13'b0, dmaor[2:1] & (reg_wdata[2:1] | ~error_seen), 1'b0};
                error_seen <= 0;
            end
            if (!dmaor[15]) begin
                ddt_prior_valid <= 0;
                ddt_pending <= 0;
            end else begin
                if (ddt_accept && ddt_kind == DTR && (!ddt_reg_command || ddt_setup)) begin
                    ddt_prior_channel <= ddt_channel;
                    ddt_prior_valid <= 1;
                end
                if (ddt_accept && ddt_channel == 0 && (!ddt_reg_command || ddt_setup))
                    ddt_pending <= 1;
                else if (channel == 0 && ((start_unit && !chcr[0][19]) ||
                         (state == RETIRE && count[0] == 1)))
                    ddt_pending <= 0;
            end
            prior_nmi <= nmi;
            if (nmi && !prior_nmi) begin
                dmaor[1] <= 1;
                error_seen[0] <= 0;
            end
            dreq_meta <= dreq_n;
            dreq_sync <= dreq_meta;
            dreq_last <= dreq_sync;
            for (int i = 0; i < 2; i++)
                if (dmaor[15] || !chcr[i][0])
                    edge_pending[i] <= 0;
                else if (dreq_last[i] && !dreq_sync[i])
                    edge_pending[i] <= 1;
            dreq_ack <= 0;
            capture_accept <= 0;

            case (state)
                IDLE: if (!pause && have_request && !write_reg && !(ddt_setup || ddt_end)) begin
                    channel <= chosen;
                    state <= CAPTURE;
                end else if (!have_request)
                    round_robin <= 0;
                CAPTURE: begin
                    source <= sar[channel];
                    destination <= dar[channel];
                    {dest_mode, source_mode, resource_mode, transfer_burst, size_code} <= chcr[channel][15:4];
                    beat <= 0;
                    state <= CHECK;
                end
                CHECK: if (stop_unit)
                    state <= IDLE;
                else if (address_error) begin
                    dmaor[2] <= 1;
                    error_seen[1] <= 0;
                    state <= IDLE;
                end else begin
                    if (external_enabled[channel] && !dmaor[15] && !channel[1]) begin
                        if (!(chcr[channel][19] && transfer_burst))
                            edge_pending[channel[0]] <= 0;
                        dreq_ack[channel[0]] <= 1;
                    end
                    if (resource_mode inside {4'd12, 4'd14})
                        capture_accept <= 1;
                    if (captured_source) begin
                        buffer[0] <= capture_value;
                        state <= WRITE_REQUEST;
                    end else
                        state <= single_write ? DEVICE_READ : READ_REQUEST;
                end
                DEVICE_READ: if (device_ready) begin
                    buffer[beat] <= device_rdata;
                    if (device_last) begin
                        beat <= 0;
                        state <= WRITE_REQUEST;
                    end else
                        beat <= beat + 3'd1;
                end
                DEVICE_WRITE: if (device_ready) begin
                    beat <= beat + 3'd1;
                    if (device_last)
                        state <= RETIRE;
                end
                READ_REQUEST: if (mem_ready)
                    state <= READ_REPLY;
                READ_REPLY: if (mem_rvalid) begin
                    buffer[beat] <= mem_rdata >> {source_lane, 3'b0};
                    if (beat == last_beat) begin
                        beat <= 0;
                        state <= single_read ? DEVICE_WRITE : WRITE_REQUEST;
                    end else begin
                        beat <= beat + 3'd1;
                        state <= READ_REQUEST;
                    end
                end
                WRITE_REQUEST: if (mem_ready) begin
                    beat <= beat + 3'd1;
                    if (beat == last_beat)
                        state <= RETIRE;
                end
                RETIRE: begin
                    if (!single_write && source_mode != 0)
                        sar[channel] <= source_mode == 1 ? next_source : source - {26'b0, unit_bytes};
                    if (!single_read && dest_mode != 0)
                        dar[channel] <= dest_mode == 1 ? next_destination : destination - {26'b0, unit_bytes};
                    count[channel] <= count[channel] - 24'd1;
                    if (count[channel] == 1) begin
                        if (!channel[1] && !dmaor[15] && external_enabled[channel])
                            edge_pending[channel[0]] <= 0;
                        chcr[channel][1] <= 1;
                        end_seen[channel] <= 0;
                    end
                    round_robin <= channel + 2'd1;
                    state <= IDLE;
                end
                default: state <= IDLE;
            endcase
        end
    end
endmodule
