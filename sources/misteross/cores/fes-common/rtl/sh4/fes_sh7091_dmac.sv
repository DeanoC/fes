// SH7091 normal-mode DMA. Each transfer unit is read into a buffer before
// writes begin, so a wide unit can travel over the 32-bit external port.
module fes_sh7091_dmac (
    input  logic        clk, reset, nmi,
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
    output logic        mem_valid, mem_burst,
    output logic [1:0]  mem_serial,
    input  logic        mem_ready,
    output logic [31:0] mem_addr, mem_wdata,
    output logic [3:0]  mem_rmask, mem_wstrb,
    input  logic        mem_rvalid,
    input  logic [31:0] mem_rdata
);
    typedef enum logic [2:0] {IDLE, CAPTURE, CHECK, READ_REQUEST, READ_REPLY, WRITE_REQUEST, RETIRE} state_t;
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

    // Channel registers and DMAOR are longword P4 accesses.
    assign reg_channel = reg_addr[5:4];
    assign op_register = reg_addr[6];
    assign reg_selected = reg_addr[31:7] == 25'h1ff4000 && (!op_register || reg_addr[5:2] == 0);
    assign write_reg = reg_request && reg_selected && reg_wstrb == 4'hf;
    assign read_reg = reg_request && reg_selected && reg_wstrb == 0 && reg_rmask == 4'hf;
    assign channel_write = write_reg && !op_register && (reg_channel != 0 || !dmaor[15]);

    for (genvar i = 0; i < 4; i++) begin : request_channel
        logic external_pending;
        if (i < 2) begin : has_external_pin
            assign external_pending = chcr[i][19] ? edge_pending[i] : !dreq_sync[i];
        end else begin : internal_requests_only
            assign external_pending = 0;
        end
        assign eligible[i] = chcr[i][0] && !chcr[i][1] &&
                             ((chcr[i][11:8] == 0 && external_pending) ||
                              chcr[i][11:8] == 4 ||
                              (chcr[i][11:10] == 2'b10 && serial_request[chcr[i][9:8]]) ||
                              ((chcr[i][11:8] inside {4'd12, 4'd14}) && capture_pending));
        assign irq[i] = chcr[i][2] && chcr[i][1];
    end
    assign irq[4] = dmaor[2];
    assign have_request = dmaor[0] && dmaor[2:1] == 0 && !dmaor[15] && (|eligible);

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
    assign address_error = size_code > 4 || source_mode == 3 || dest_mode == 3 ||
                           |((source[4:0] | destination[4:0]) & alignment_mask) ||
                           (serial_tx ? destination[28:0] != serial_tx_addr || size_code != 1 :
                                      destination[28:26] == 3'b111) ||
                           (serial_rx ? source[28:0] != serial_rx_addr || size_code != 1 :
                            captured_source ? source != 32'hffd8_002c || size_code != 3 :
                                              source[28:26] == 3'b111);
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
            if (channel_write) case (reg_addr[3:2])
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
            prior_nmi <= nmi;
            if (nmi && !prior_nmi) begin
                dmaor[1] <= 1;
                error_seen[0] <= 0;
            end
            dreq_meta <= dreq_n;
            dreq_sync <= dreq_meta;
            dreq_last <= dreq_sync;
            for (int i = 0; i < 2; i++)
                if (chcr[i][0] && dreq_last[i] && !dreq_sync[i])
                    edge_pending[i] <= 1;
            dreq_ack <= 0;
            capture_accept <= 0;

            case (state)
                IDLE: if (have_request && !write_reg) begin
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
                CHECK: if (!dmaor[0] || dmaor[2:1] != 0 || !chcr[channel][0] || (nmi && !prior_nmi))
                    state <= IDLE;
                else if (address_error) begin
                    dmaor[2] <= 1;
                    error_seen[1] <= 0;
                    state <= IDLE;
                end else begin
                    if (resource_mode == 0 && !channel[1]) begin
                        edge_pending[channel[0]] <= 0;
                        dreq_ack[channel[0]] <= 1;
                    end
                    if (resource_mode inside {4'd12, 4'd14})
                        capture_accept <= 1;
                    if (captured_source) begin
                        buffer[0] <= capture_value;
                        state <= WRITE_REQUEST;
                    end else
                        state <= READ_REQUEST;
                end
                READ_REQUEST: if (mem_ready)
                    state <= READ_REPLY;
                READ_REPLY: if (mem_rvalid) begin
                    buffer[beat] <= mem_rdata >> {source_lane, 3'b0};
                    if (beat == last_beat) begin
                        beat <= 0;
                        state <= WRITE_REQUEST;
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
                    if (source_mode != 0)
                        sar[channel] <= source_mode == 1 ? next_source : source - {26'b0, unit_bytes};
                    if (dest_mode != 0)
                        dar[channel] <= dest_mode == 1 ? next_destination : destination - {26'b0, unit_bytes};
                    count[channel] <= count[channel] - 24'd1;
                    if (count[channel] == 1) begin
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
