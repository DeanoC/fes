// HPS-controlled diagnostic slot. No programming or DMA engine is present.
`include "generated/fes_de25_pr_control.vh"
`ifndef FES_DE25_PR_BUILD_ID
`define FES_DE25_PR_BUILD_ID 128'd0
`endif
module fes_link_shell #(
    parameter [127:0] BUILD_ID = `FES_DE25_PR_BUILD_ID,
    parameter integer TIMEOUT_CYCLES = `FES_DE25_PR_CONTROL_TIMEOUT_CYCLES
) (
    input wire clk, reset_n,
    input wire [5:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata = 0,
    output reg readdatavalid = 0,
    output reg writeresponsevalid = 0,
    output reg [1:0] response = 0,
    output wire waitrequest,
    output reg req = 0,
    output reg [7:0] slot_address = 0,
    (* preserve, dont_merge *) output reg slot_reset = 0,
    input wire rsp_valid,
    input wire [31:0] rsp_data,
    output wire frozen, running,
    output wire [31:0] isolated_data,
    output reg [31:0] static_count = 0,
    output reg [31:0] retired = 0,
    output reg [31:0] last_data = 0
);
    localparam [2:0] FROZEN = 0, RUNNING = 1, DRAIN = 2, RESET_SETTLE = 3,
                     RELEASE_SETTLE = 4, PROBE = 5, FAILED = 6;
    localparam [31:0] PERSONA_A = `FES_DE25_PR_CONTROL_PERSONA_A, PERSONA_B = `FES_DE25_PR_CONTROL_PERSONA_B;
    reg [2:0] state = RESET_SETTLE;
    reg first_seen = 0, healthy = 0;
    reg [31:0] fault_flags = 0, argument = 0;
    reg argument_valid = 0;
    reg [23:0] completed_seq = 0, active_seq = 0;
    reg [31:0] persona_id = 0, expected_persona = 0, elapsed = 0;
    reg [2:0] settle = 4, pending = 0;
    // Every PR egress terminates at a static-region data register.
    (* preserve, dont_merge *) reg response_valid_q = 0;
    (* preserve, dont_merge *) reg [31:0] response_data_q = 0;
    reg response_has_request_q = 0;

    // A late peripheral reset cannot manufacture a fresh controller epoch.
    always @(posedge clk) if (reset_n) first_seen <= 1;
    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) healthy <= 0;
        else if (!first_seen) healthy <= 1;
    end
    always @(posedge clk) begin
        response_valid_q <= rsp_valid;
        response_data_q <= rsp_data;
        // Keep the PR port directly registered at the static boundary. The
        // marker captures whether a request was already outstanding on the
        // edge that captures rsp_valid into response_valid_q.
        response_has_request_q <= pending != 0;
    end
    wire [31:0] faults = fault_flags | ((first_seen && !healthy) ?
        `FES_DE25_PR_CONTROL_FAULT_RESET : 32'd0);
    wire faulted = faults != 0;
    wire ready = reset_n && first_seen && healthy && !faulted;
    wire busy = state != FROZEN && state != RUNNING && state != FAILED;
    assign frozen = ready && state == FROZEN;
    assign running = ready && state == RUNNING;
    assign isolated_data = running ? response_data_q : 32'd0;
    assign waitrequest = !reset_n || !first_seen;
    wire [31:0] status = (ready ? `FES_DE25_PR_CONTROL_STATUS_READY : 0) |
        (busy ? `FES_DE25_PR_CONTROL_STATUS_BUSY : 0) |
        (frozen ? `FES_DE25_PR_CONTROL_STATUS_FROZEN : 0) |
        (running ? `FES_DE25_PR_CONTROL_STATUS_RUNNING : 0) |
        (faulted ? `FES_DE25_PR_CONTROL_STATUS_FAULT : 0);
    wire [7:0] opcode = writedata[31:24];
    wire [23:0] command_seq = writedata[23:0];
    wire accept = !waitrequest;
    wire accepted_read = read && accept;
    wire accepted_write = write && accept;
    wire [31:0] byte_address = {26'd0, address};
    // Configuration may drive arbitrary PR outputs. The static D registers
    // remain the only boundary sinks; no captured value is consumed while the
    // acknowledged slot is frozen or either reset pipeline is settling. Drain
    // was already proved before entering RESET_SETTLE; cold PR FFs may be random.
    wire response_active = state == RUNNING || state == DRAIN || state == PROBE;
    wire retire_valid = response_valid_q && response_active;
    // The fixed-latency slot can never answer a request accepted on this edge.
    wire valid_reply = retire_valid && pending != 0 && response_has_request_q;
    wire unsolicited_reply = retire_valid && (!response_has_request_q || pending == 0);
    reg [31:0] read_value;
    reg read_address_valid;
    reg [31:0] access_fault;
    always @* begin
        read_address_valid = 1;
        case (byte_address)
            `FES_DE25_PR_CONTROL_ID_OFFSET: read_value = `FES_DE25_PR_CONTROL_IDENTITY;
            `FES_DE25_PR_CONTROL_VERSION_OFFSET: read_value = `FES_DE25_PR_CONTROL_VERSION;
            `FES_DE25_PR_CONTROL_STATUS_OFFSET: read_value = status;
            `FES_DE25_PR_CONTROL_ARGUMENT_OFFSET: read_value = argument_valid ? argument : 32'd0;
            `FES_DE25_PR_CONTROL_COMMAND_OFFSET: read_value = 0;
            `FES_DE25_PR_CONTROL_FAULT_OFFSET: read_value = faults;
            `FES_DE25_PR_CONTROL_COMPLETED_SEQ_OFFSET: read_value = {8'd0, completed_seq};
            `FES_DE25_PR_CONTROL_STATIC_COUNT_OFFSET: read_value = static_count;
            `FES_DE25_PR_CONTROL_RETIRED_OFFSET: read_value = retired;
            `FES_DE25_PR_CONTROL_LAST_DATA_OFFSET: read_value = last_data;
            `FES_DE25_PR_CONTROL_PERSONA_ID_OFFSET: read_value = persona_id;
            `FES_DE25_PR_CONTROL_BUILD_ID0_OFFSET: read_value = BUILD_ID[127:96];
            `FES_DE25_PR_CONTROL_BUILD_ID1_OFFSET: read_value = BUILD_ID[95:64];
            `FES_DE25_PR_CONTROL_BUILD_ID2_OFFSET: read_value = BUILD_ID[63:32];
            `FES_DE25_PR_CONTROL_BUILD_ID3_OFFSET: read_value = BUILD_ID[31:0];
            `FES_DE25_PR_CONTROL_TIMEOUT_CYCLES_OFFSET: read_value = TIMEOUT_CYCLES;
            default: begin read_address_valid = 0; read_value = 0; end
        endcase
        access_fault = 0;
        if (accepted_read && (!read_address_valid || write))
            access_fault = `FES_DE25_PR_CONTROL_FAULT_ADDRESS;
        if (accepted_write) begin
            if (!ready) access_fault = faults;
            else if (busy) access_fault = `FES_DE25_PR_CONTROL_FAULT_BUSY;
            else if (read || (byte_address != `FES_DE25_PR_CONTROL_ARGUMENT_OFFSET &&
                              byte_address != `FES_DE25_PR_CONTROL_COMMAND_OFFSET))
                access_fault = `FES_DE25_PR_CONTROL_FAULT_ADDRESS;
            else if (byteenable != 4'hf)
                access_fault = `FES_DE25_PR_CONTROL_FAULT_PARTIAL;
            else if (byte_address == `FES_DE25_PR_CONTROL_COMMAND_OFFSET) begin
                if (!argument_valid || command_seq == 0 || completed_seq == 24'hffffff || command_seq != completed_seq + 24'd1)
                    access_fault = `FES_DE25_PR_CONTROL_FAULT_SEQUENCE;
                else if (opcode == 8'd1 && argument != 0)
                    access_fault = `FES_DE25_PR_CONTROL_FAULT_PROTOCOL;
                else if (opcode == 8'd2 && (state != FROZEN || (argument != PERSONA_A && argument != PERSONA_B)))
                    access_fault = `FES_DE25_PR_CONTROL_FAULT_PERSONA;
                else if (opcode != 8'd1 && opcode != 8'd2)
                    access_fault = `FES_DE25_PR_CONTROL_FAULT_PROTOCOL;
            end
        end
    end

    always @(posedge clk) begin
        static_count <= static_count + 32'd1;
        req <= 0;
        // Use the fabric's native low power-up level on this boundary FF.
        // RESET_SETTLE asserts it before any request or FROZEN acknowledgement;
        // a high power-up level would add an inverter at the PR boundary.
        if (state == RESET_SETTLE) slot_reset <= 1;
        readdatavalid <= accepted_read;
        writeresponsevalid <= accepted_write;
        response <= (access_fault != 0 || (accepted_write && faulted)) ? 2'b10 : 2'b00;
        if (accepted_read) readdata <= read_value;
        if (ready) begin
            case ({req, valid_reply})
                2'b10: pending <= pending + 3'd1;
                2'b01: pending <= pending - 3'd1;
                default: pending <= pending;
            endcase
            if (valid_reply) begin
                retired <= retired + 32'd1;
                last_data <= response_data_q;
            end
            if (busy) elapsed <= elapsed + 32'd1;
            if (accepted_write && access_fault == 0) begin
                if (byte_address == `FES_DE25_PR_CONTROL_ARGUMENT_OFFSET) begin
                    argument <= writedata;
                    argument_valid <= 1;
                end else begin
                    active_seq <= command_seq;
                    argument_valid <= 0;
                    elapsed <= 0;
                    if (opcode == 8'd1) begin
                        if (state == FROZEN) completed_seq <= command_seq;
                        else state <= DRAIN;
                    end else begin
                        expected_persona <= argument;
                        persona_id <= 0;
                        slot_reset <= 0;
                        settle <= 3'd4;
                        state <= RELEASE_SETTLE;
                    end
                end
            end else begin
                case (state)
                    RUNNING: if (static_count[3:0] == 0) begin
                        req <= 1;
                        slot_address <= slot_address == 8'hff ? 8'd1 : slot_address + 8'd1;
                    end
                    DRAIN: if (pending == 0 && !req && !response_valid_q) begin
                        slot_reset <= 1;
                        settle <= 3'd4;
                        state <= RESET_SETTLE;
                    end
                    RESET_SETTLE: if (settle != 0) settle <= settle - 3'd1;
                        else begin state <= FROZEN; completed_seq <= active_seq; end
                    RELEASE_SETTLE: if (settle != 0) settle <= settle - 3'd1;
                        else begin req <= 1; slot_address <= 0; state <= PROBE; end
                    PROBE: if (valid_reply && response_data_q == expected_persona) begin
                        persona_id <= response_data_q;
                        completed_seq <= active_seq;
                        slot_address <= 0;
                        state <= RUNNING;
                    end
                    default: begin end
                endcase
            end
        end
        if (faulted || access_fault != 0 ||
            (ready && unsolicited_reply) ||
            (ready && req && pending == 3'd7 && !valid_reply) ||
            (ready && busy && elapsed >= TIMEOUT_CYCLES - 1) ||
            (ready && state == PROBE && retire_valid && response_data_q != expected_persona)) begin
            fault_flags <= faults | access_fault |
                ((ready && unsolicited_reply) ||
                 (ready && req && pending == 3'd7 && !valid_reply) ? `FES_DE25_PR_CONTROL_FAULT_PROTOCOL : 0) |
                (ready && busy && elapsed >= TIMEOUT_CYCLES - 1 ? `FES_DE25_PR_CONTROL_FAULT_TIMEOUT : 0) |
                (ready && state == PROBE && retire_valid && response_data_q != expected_persona ? `FES_DE25_PR_CONTROL_FAULT_PERSONA : 0);
            req <= 0;
            slot_reset <= 1;
            argument_valid <= 0;
            // A same-edge response/settle completion loses to any fault.
            completed_seq <= completed_seq;
            persona_id <= persona_id;
            state <= FAILED;
        end
    end
endmodule
