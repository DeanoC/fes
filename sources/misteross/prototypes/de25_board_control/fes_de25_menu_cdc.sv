// SPDX-License-Identifier: GPL-3.0-or-later
// One bounded Avalon request crosses to the pixel owner. Four-phase return
// holds both packets stable; a reset/timeout retires rather than replaying.
module fes_de25_menu_cdc #(
    parameter integer TIMEOUT_CYCLES = 1048576
)(
    input wire clk, reset_n, pixel_clk, pixel_reset_n,
    input wire [7:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output wire waitrequest,
    output wire [31:0] readdata,
    output reg readdatavalid, writeresponsevalid,
    output wire [1:0] response,
    output reg failed,
    output wire pixel_core_reset_n,
    output wire command_valid, command_write,
    output wire [7:0] command_address,
    output wire [31:0] command_wdata,
    output wire [3:0] command_wstrb,
    input wire command_ready, result_valid, result_error,
    input wire [31:0] result_data
);
    wire pixel_async_reset_n = reset_n && pixel_reset_n;
    (* preserve, dont_merge *) reg [1:0] pixel_release;
    always @(posedge pixel_clk or negedge pixel_async_reset_n)
        if (!pixel_async_reset_n) pixel_release <= 0;
        else pixel_release <= {pixel_release[0],1'b1};
    assign pixel_core_reset_n = pixel_release[1];

    // Asynchronous assertion catches even a reset pulse shorter than clk.
    // After first use, its observation permanently retires the CPU side.
    (* preserve, dont_merge *) reg [1:0] pixel_reset_seen;
    always @(posedge clk or negedge pixel_async_reset_n)
        if (!pixel_async_reset_n) pixel_reset_seen <= 2'b11;
        else pixel_reset_seen <= {pixel_reset_seen[0],1'b0};

    (* preserve, dont_merge *) reg [44:0] request_packet, command_packet;
    (* preserve, dont_merge *) reg [32:0] response_packet, reply_packet;
    reg request_valid, acknowledge, pixel_online;
    (* preserve, dont_merge, altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] request_sync;
    (* preserve, dont_merge, altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] acknowledge_sync, online_sync;
    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) begin acknowledge_sync<=0; online_sync<=0; end
        else begin acknowledge_sync<={acknowledge_sync[0],acknowledge}; online_sync<={online_sync[0],pixel_online}; end
    end
    always @(posedge pixel_clk)
        if (!pixel_core_reset_n) request_sync<=0;
        else request_sync<={request_sync[0],request_valid};

    localparam [1:0] CPU_IDLE=0, CPU_WAIT=1, CPU_RETURN=2;
    reg [1:0] cpu_state;
    reg used, pending_read, pending_write;
    localparam integer TIMER_BITS = TIMEOUT_CYCLES > 1 ? $clog2(TIMEOUT_CYCLES) : 1;
    reg [TIMER_BITS-1:0] timer;
    assign waitrequest = !reset_n || cpu_state!=CPU_IDLE;
    // Some generated masters discard Avalon response codes. An error must
    // still be observable in the native identity/status reads; do not let its
    // only physical use disappear when that optional response port is pruned.
    assign readdata = reply_packet[32] ? 32'hffffffff : reply_packet[31:0];
    assign response = reply_packet[32] ? 2'b10 : 2'b00;
    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) begin
            cpu_state<=CPU_IDLE; used<=0; failed<=0; request_valid<=0;
            request_packet<=0; reply_packet<=0; timer<=0;
            pending_read<=0; pending_write<=0; readdatavalid<=0; writeresponsevalid<=0;
        end else begin
            readdatavalid<=0; writeresponsevalid<=0;
            if (used && pixel_reset_seen[1]) failed<=1;
            case (cpu_state)
                CPU_IDLE: if (read || write) begin
                    used<=1;
                    if (failed || pixel_reset_seen[1] || !online_sync[1] || acknowledge_sync[1] ||
                        (read && write) || TIMEOUT_CYCLES<2) begin
                        failed<=1; reply_packet<={1'b1,32'd0};
                        readdatavalid<=read; writeresponsevalid<=write;
                        cpu_state<=CPU_RETURN;
                    end else begin
                        request_packet<={write,address,writedata,byteenable};
                        request_valid<=1; pending_read<=read; pending_write<=write;
                        timer<=0; cpu_state<=CPU_WAIT;
                    end
                end
                CPU_WAIT: begin
                    if (pixel_reset_seen[1] || !online_sync[1] || timer==TIMER_BITS'(TIMEOUT_CYCLES-1)) begin
                        failed<=1; request_valid<=0; reply_packet<={1'b1,32'd0};
                        readdatavalid<=pending_read; writeresponsevalid<=pending_write;
                        cpu_state<=CPU_RETURN;
                    end else if (acknowledge_sync[1]) begin
                        request_valid<=0; reply_packet<=response_packet;
                        readdatavalid<=pending_read; writeresponsevalid<=pending_write;
                        cpu_state<=CPU_RETURN;
                    end else timer<=timer+1'b1;
                end
                CPU_RETURN: if (!acknowledge_sync[1]) cpu_state<=CPU_IDLE;
                default: begin failed<=1; request_valid<=0; cpu_state<=CPU_IDLE; end
            endcase
        end
    end

    localparam [2:0] PIXEL_ARM=0, PIXEL_READY=1, PIXEL_ISSUE=2,
        PIXEL_RESPONSE=3, PIXEL_RETURN=4;
    reg [2:0] pixel_state;
    reg [1:0] arm_count;
    assign command_valid = pixel_state==PIXEL_ISSUE && request_sync[1];
    assign {command_write,command_address,command_wdata,command_wstrb} = command_packet;
    always @(posedge pixel_clk) begin
        if (!pixel_core_reset_n) begin
            pixel_state<=PIXEL_ARM; pixel_online<=0; acknowledge<=0;
            command_packet<=0; response_packet<=0; arm_count<=0;
        end else begin
        if (arm_count!=3) arm_count<=arm_count+1'b1;
        case (pixel_state)
            // Never adopt a request that survived a pixel-only reset. Wait
            // for its explicit withdrawal before announcing a fresh owner.
            PIXEL_ARM: if (arm_count==3 && !request_sync[1]) begin pixel_online<=1; pixel_state<=PIXEL_READY; end
            PIXEL_READY: if (request_sync[1]) begin command_packet<=request_packet; pixel_state<=PIXEL_ISSUE; end
            PIXEL_ISSUE: if (!request_sync[1]) pixel_state<=PIXEL_ARM;
            else if (command_ready) begin
                if (result_valid) begin
                    response_packet<={result_error,result_data}; acknowledge<=1; pixel_state<=PIXEL_RETURN;
                end else pixel_state<=PIXEL_RESPONSE;
            end
            PIXEL_RESPONSE: if (result_valid) begin
                response_packet<={result_error,result_data}; acknowledge<=1; pixel_state<=PIXEL_RETURN;
            end
            PIXEL_RETURN: if (!request_sync[1]) begin acknowledge<=0; pixel_state<=PIXEL_READY; end
            default: begin pixel_online<=0; pixel_state<=PIXEL_ARM; end
        endcase
        end
    end
endmodule
