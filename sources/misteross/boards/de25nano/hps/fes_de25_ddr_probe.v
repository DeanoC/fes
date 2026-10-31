// Read-only, bounded DMA-bus diagnostic. No physical DDR address is implied.
// Software cancel drains local traffic; hard reset requires an external lifetime gate.
`include "generated/fes_de25_ddr_probe.vh"
module fes_de25_ddr_probe (
    input wire clk, reset_n,
    input wire [`FES_DE25_DDR_PROBE_ADDRESS_BITS-1:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata,
    output reg readdatavalid,
    output wire waitrequest,
    output reg [31:0] avm_address,
    output reg [4:0] avm_burstcount,
    output reg avm_read,
    output wire [15:0] avm_byteenable,
    input wire avm_waitrequest,
    input wire [127:0] avm_readdata,
    input wire avm_readdatavalid,
    input wire [1:0] avm_response
);
    reg [31:0] staged [0:7];
    reg busy, done, fault, cancelled, stopping;
    reg [31:0] completed_bytes, accepted_bytes, checksum, errors;
    reg [31:0] active_sequence;
    reg [63:0] active_generation;
    reg [127:0] last_data;
    reg [31:0] next_address, remaining_bytes;
    reg [4:0] outstanding;
    integer lane, field;

    wire [31:0] byte_address = {{(32-`FES_DE25_DDR_PROBE_ADDRESS_BITS){1'b0}}, address};
    wire locally_idle = !busy && !avm_read && outstanding == 5'd0;
    wire [31:0] status = (locally_idle ? `FES_DE25_DDR_PROBE_STATUS_IDLE : 32'd0) |
        (busy ? `FES_DE25_DDR_PROBE_STATUS_BUSY : 32'd0) |
        (done ? `FES_DE25_DDR_PROBE_STATUS_DONE : 32'd0) |
        (fault ? `FES_DE25_DDR_PROBE_STATUS_FAULT : 32'd0) |
        (cancelled ? `FES_DE25_DDR_PROBE_STATUS_CANCELLED : 32'd0);
    wire [31:0] enabled_command = writedata & {{8{byteenable[3]}}, {8{byteenable[2]}},
                                               {8{byteenable[1]}}, {8{byteenable[0]}}};
    wire command_write = write && |byteenable && byte_address == `FES_DE25_DDR_PROBE_COMMAND_OFFSET;
    wire command_start = command_write && enabled_command == `FES_DE25_DDR_PROBE_COMMAND_START;
    wire command_cancel = command_write && enabled_command == `FES_DE25_DDR_PROBE_COMMAND_CANCEL;
    wire command_clear = command_write && enabled_command == `FES_DE25_DDR_PROBE_COMMAND_CLEAR;
    wire [32:0] grant_end = {1'b0, staged[0]} + {1'b0, staged[2]};
    wire [32:0] read_end = {1'b0, staged[3]} + {1'b0, staged[4]};
    wire descriptor_valid = staged[1] == 32'd0 && staged[0][4:0] == 5'd0 &&
        staged[2] != 32'd0 && staged[2][3:0] == 4'd0 &&
        staged[3][3:0] == 4'd0 && staged[4] != 32'd0 && staged[4][3:0] == 4'd0 &&
        grant_end <= 33'h100000000 && read_end <= {1'b0, staged[2]} &&
        {staged[6], staged[5]} != 64'd0 && staged[7] != 32'd0;
    wire accepting = avm_read && !avm_waitrequest;
    // Include a newly accepted burst so a zero-latency first response cannot
    // escape accounting. There is still at most one outstanding burst.
    wire [5:0] response_budget = {1'b0, outstanding} + (accepting ? {1'b0, avm_burstcount} : 6'd0);
    wire returning = avm_readdatavalid && response_budget != 6'd0;
    reg [31:0] new_errors;
    always @* begin
        new_errors = 32'd0;
        if (command_write && !command_start && !command_cancel && !command_clear)
            new_errors = new_errors | `FES_DE25_DDR_PROBE_ERROR_COMMAND;
        if ((command_start || command_clear) && !locally_idle)
            new_errors = new_errors | `FES_DE25_DDR_PROBE_ERROR_BUSY_COMMAND;
        if (command_start && locally_idle) begin
            if (fault) new_errors = new_errors | `FES_DE25_DDR_PROBE_ERROR_COMMAND;
            else if (!descriptor_valid) new_errors = new_errors | `FES_DE25_DDR_PROBE_ERROR_DESCRIPTOR;
        end
        if (avm_readdatavalid && response_budget == 6'd0)
            new_errors = new_errors | `FES_DE25_DDR_PROBE_ERROR_UNEXPECTED_RESPONSE;
        if (returning && avm_response != 2'b00)
            new_errors = new_errors | `FES_DE25_DDR_PROBE_ERROR_RESPONSE;
    end
    wire halt_event = command_cancel || new_errors != 32'd0;
    wire [12:0] boundary_bytes = 13'd4096 - {1'b0, next_address[11:0]};
    wire [4:0] boundary_burst = boundary_bytes >= 13'd256 ? 5'd16 : boundary_bytes[8:4];
    wire [4:0] next_burst = remaining_bytes >= {23'd0, boundary_burst, 4'd0} ?
                           boundary_burst : remaining_bytes[8:4];
    wire [31:0] accepted_burst_bytes = {23'd0, avm_burstcount, 4'd0};
    reg stage_write;
    reg [2:0] stage_index;
    always @* begin
        stage_write = 1'b1;
        stage_index = 3'd0;
        case (byte_address)
            `FES_DE25_DDR_PROBE_GRANT_LO_OFFSET: stage_index = 3'd0;
            `FES_DE25_DDR_PROBE_GRANT_HI_OFFSET: stage_index = 3'd1;
            `FES_DE25_DDR_PROBE_GRANT_SPAN_OFFSET: stage_index = 3'd2;
            `FES_DE25_DDR_PROBE_READ_OFFSET_OFFSET: stage_index = 3'd3;
            `FES_DE25_DDR_PROBE_READ_BYTES_OFFSET: stage_index = 3'd4;
            `FES_DE25_DDR_PROBE_GENERATION_LO_OFFSET: stage_index = 3'd5;
            `FES_DE25_DDR_PROBE_GENERATION_HI_OFFSET: stage_index = 3'd6;
            `FES_DE25_DDR_PROBE_SEQUENCE_OFFSET: stage_index = 3'd7;
            default: stage_write = 1'b0;
        endcase
    end
    assign waitrequest = !reset_n;
    assign avm_byteenable = 16'hffff;

    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) begin
            for (field = 0; field < 8; field = field + 1) staged[field] <= 32'd0;
            readdata <= 32'd0;
            readdatavalid <= 1'b0;
            busy <= 1'b0; done <= 1'b0; fault <= 1'b0; cancelled <= 1'b0; stopping <= 1'b0;
            completed_bytes <= 32'd0; accepted_bytes <= 32'd0;
            checksum <= 32'd0; errors <= 32'd0; last_data <= 128'd0;
            active_sequence <= 32'd0; active_generation <= 64'd0;
            next_address <= 32'd0; remaining_bytes <= 32'd0; outstanding <= 5'd0;
            avm_address <= 32'd0; avm_burstcount <= 5'd0; avm_read <= 1'b0;
        end else begin
            readdatavalid <= read;
            if (read) begin
                case (byte_address)
                    `FES_DE25_DDR_PROBE_ID_OFFSET: readdata <= `FES_DE25_DDR_PROBE_IDENTITY;
                    `FES_DE25_DDR_PROBE_VERSION_OFFSET: readdata <= `FES_DE25_DDR_PROBE_VERSION;
                    `FES_DE25_DDR_PROBE_STATUS_OFFSET: readdata <= status;
                    `FES_DE25_DDR_PROBE_GRANT_LO_OFFSET: readdata <= staged[0];
                    `FES_DE25_DDR_PROBE_GRANT_HI_OFFSET: readdata <= staged[1];
                    `FES_DE25_DDR_PROBE_GRANT_SPAN_OFFSET: readdata <= staged[2];
                    `FES_DE25_DDR_PROBE_READ_OFFSET_OFFSET: readdata <= staged[3];
                    `FES_DE25_DDR_PROBE_READ_BYTES_OFFSET: readdata <= staged[4];
                    `FES_DE25_DDR_PROBE_GENERATION_LO_OFFSET: readdata <= staged[5];
                    `FES_DE25_DDR_PROBE_GENERATION_HI_OFFSET: readdata <= staged[6];
                    `FES_DE25_DDR_PROBE_SEQUENCE_OFFSET: readdata <= staged[7];
                    `FES_DE25_DDR_PROBE_COMPLETED_BYTES_OFFSET: readdata <= completed_bytes;
                    `FES_DE25_DDR_PROBE_CHECKSUM_OFFSET: readdata <= checksum;
                    `FES_DE25_DDR_PROBE_ERROR_OFFSET: readdata <= errors;
                    `FES_DE25_DDR_PROBE_ACTIVE_SEQUENCE_OFFSET: readdata <= active_sequence;
                    `FES_DE25_DDR_PROBE_ACTIVE_GENERATION_LO_OFFSET: readdata <= active_generation[31:0];
                    `FES_DE25_DDR_PROBE_ACTIVE_GENERATION_HI_OFFSET: readdata <= active_generation[63:32];
                    `FES_DE25_DDR_PROBE_LAST_DATA0_OFFSET: readdata <= last_data[31:0];
                    `FES_DE25_DDR_PROBE_LAST_DATA1_OFFSET: readdata <= last_data[63:32];
                    `FES_DE25_DDR_PROBE_LAST_DATA2_OFFSET: readdata <= last_data[95:64];
                    `FES_DE25_DDR_PROBE_LAST_DATA3_OFFSET: readdata <= last_data[127:96];
                    `FES_DE25_DDR_PROBE_ACCEPTED_BYTES_OFFSET: readdata <= accepted_bytes;
                    default: readdata <= 32'd0;
                endcase
            end
            if (write && stage_write)
                for (lane = 0; lane < 4; lane = lane + 1)
                    if (byteenable[lane]) staged[stage_index][8*lane +: 8] <= writedata[8*lane +: 8];

            if (accepting) begin
                avm_read <= 1'b0;
                accepted_bytes <= accepted_bytes + accepted_burst_bytes;
                remaining_bytes <= remaining_bytes - accepted_burst_bytes;
                next_address <= next_address + accepted_burst_bytes;
            end
            if (returning) begin
                completed_bytes <= completed_bytes + 32'd16;
                checksum <= checksum ^ avm_readdata[31:0] ^ avm_readdata[63:32] ^
                                       avm_readdata[95:64] ^ avm_readdata[127:96];
                last_data <= avm_readdata;
            end
            if (accepting || returning) outstanding <= response_budget[4:0] - {4'd0, returning};

            // A presented command is never withdrawn during waitrequest,
            // even if software cancels or a fault arrives meanwhile.
            if (busy && !stopping && !halt_event && !avm_read && outstanding == 5'd0 && remaining_bytes != 32'd0) begin
                avm_address <= next_address;
                avm_burstcount <= next_burst;
                avm_read <= 1'b1;
            end
            if (busy && !avm_read && outstanding == 5'd0 &&
                    (remaining_bytes == 32'd0 || stopping || halt_event)) begin
                busy <= 1'b0;
                done <= remaining_bytes == 32'd0 && !stopping && !halt_event && !fault && !cancelled;
            end
            if (command_cancel && !locally_idle) begin
                cancelled <= 1'b1;
                stopping <= 1'b1;
                done <= 1'b0;
            end
            if (command_clear && locally_idle && new_errors == 32'd0) begin
                done <= 1'b0; fault <= 1'b0; cancelled <= 1'b0; stopping <= 1'b0;
                completed_bytes <= 32'd0; accepted_bytes <= 32'd0;
                checksum <= 32'd0; errors <= 32'd0; last_data <= 128'd0;
                active_sequence <= 32'd0; active_generation <= 64'd0;
            end
            if (command_start && locally_idle && !fault && new_errors == 32'd0) begin
                busy <= 1'b1; done <= 1'b0; cancelled <= 1'b0; stopping <= 1'b0;
                completed_bytes <= 32'd0; accepted_bytes <= 32'd0;
                checksum <= 32'd0; errors <= 32'd0; last_data <= 128'd0;
                active_sequence <= staged[7]; active_generation <= {staged[6], staged[5]};
                next_address <= staged[0] + staged[3]; remaining_bytes <= staged[4];
            end
            // Faults win over a simultaneous clear/start and preserve every
            // reason observed on this edge. Drain accounting remains live.
            if (new_errors != 32'd0) begin
                errors <= errors | new_errors;
                fault <= 1'b1;
                stopping <= 1'b1;
                done <= 1'b0;
            end
        end
    end
endmodule
