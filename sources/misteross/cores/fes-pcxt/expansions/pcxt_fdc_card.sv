// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"

// IBM XT-compatible, read-only 765-family floppy adapter. One 360 KiB raw
// sector image is supplied through a separate byte service; it is not guest
// RAM. The service holds VALID and address until READY, including across
// arbitrarily long backing-store waits. Reset cancels outstanding requests.
//
// This implements the register/command subset needed for a single-sided or
// double-sided standard XT disk: DOR, MSR/data, Specify, Sense Drive, Sense
// Interrupt, Recalibrate, Seek, Read ID and DMA-mode MFM Read Data. Writes
// return write-protected status. Logical ISA timing uses the endpoint's
// prepare/commit contract; no FIFO byte is consumed during preparation.
module pcxt_fdc_card (
    input wire clk, reset, guest_ce,
    input wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] request,
    output wire [`FES_PCXT_ISA_RESPONSE_BITS-1:0] response,
    input wire media_present,
    output wire media_read_valid,
    output wire [18:0] media_read_addr,
    input wire media_read_ready,
    input wire [7:0] media_read_data
);
    // Nominal 4.772727 MHz XT guest clock: 400 ms motor spin-up, 3 ms
    // per seek step and 32 us per byte for 250 kbit/s MFM. These are guest
    // enables, not fabric cycles; stalls and host backpressure do not speed
    // up the emulated drive.
    localparam [20:0] SPIN_TICKS = 21'd1909091;
    localparam [13:0] STEP_TICKS = 14'd14318;
    localparam [7:0] BYTE_TICKS = 8'd153;
    localparam [2:0] IDLE=0, PARAMS=1, WAIT_MOTOR=2, READ_DATA=3, RESULT=4;
    reg [2:0] state;
    reg [7:0] dor, command;
    reg [7:0] params [0:7];
    reg [3:0] param_count, param_total;
    reg [7:0] result_bytes [0:6];
    reg [2:0] result_index, result_length;
    reg sense_irq_pending, result_irq_pending, sense_valid;
    reg [7:0] sense_st0, sense_cylinder;
    reg [20:0] motor_ticks;
    reg seeking, queued_seek, queued_failed, dma_mode;
    reg [7:0] queued_st0;
    reg [7:0] queued_target;
    reg [7:0] cylinder, seek_target;
    reg [19:0] seek_ticks;
    reg [7:0] read_cylinder, read_sector, read_eot, read_head;
    reg read_mt, lost_ready;
    reg [9:0] lba;
    reg [8:0] byte_index;
    reg [7:0] byte_ticks, data_byte;
    reg data_valid, fetch_pending;
    wire motor_on = dor[2] && dor[4] && dor[1:0] == 2'b00;
    wire motor_ready = motor_on && motor_ticks == SPIN_TICKS;
    assign media_read_valid = fetch_pending;
    assign media_read_addr = {lba, byte_index};

    wire active, commit;
    wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] access;
    wire [19:0] address = access[`FES_PCXT_ISA_ADDRESS_LOW +: 20];
    wire [1:0] kind = access[`FES_PCXT_ISA_KIND_LOW +: 2];
    wire write_cycle = access[`FES_PCXT_ISA_WRITE_BIT];
    wire [7:0] wdata = access[`FES_PCXT_ISA_WRITE_DATA_LOW +: 8];
    wire io = active && kind == 2'(`FES_PCXT_ISA_KIND_IO) &&
              !access[`FES_PCXT_ISA_AEN_BIT];
    // XT adapters decode the low ten I/O address bits.
    wire dor_io = io && address[9:0] == 10'h3f2;
    wire msr_io = io && address[9:0] == 10'h3f4;
    wire data_io = io && address[9:0] == 10'h3f5;
    wire dma = active && kind == 2'(`FES_PCXT_ISA_KIND_DMA) &&
               access[`FES_PCXT_ISA_AEN_BIT] &&
               access[`FES_PCXT_ISA_DMA_CHANNEL_LOW +: 2] == 2'd2;
    wire dma_data_ready = state == READ_DATA && data_valid && byte_ticks == 0;
    wire [7:0] irq = (dor[2] && dor[3] && (sense_irq_pending || result_irq_pending)) ? 8'h40 : 8'h00;
    wire [2:0] drq = (dor[2] && dor[3] && dma_data_ready) ? 3'b010 : 3'b000;
    wire [7:0] msr = state == IDLE ? 8'h80 :
                     state == PARAMS ? 8'h90 :
                     state == RESULT ? 8'hd0 : 8'h10;
    wire [7:0] msr_with_seek = msr | ((seeking || queued_seek) ? 8'h01 : 8'h00);
    wire [7:0] rdata = dma ? (dma_data_ready && !write_cycle ? data_byte : 8'hff) :
                       msr_io ? msr_with_seek :
                       data_io && state == RESULT ? result_bytes[result_index] : 8'hff;
    // Read/write and a second seek on the same drive cannot start while that
    // drive is seeking. Hold an eager port write until the existing seek
    // completes rather than use its old cylinder or discard its event.
    wire read_write_command = (wdata & 8'h1f) == 8'h02 || (wdata & 8'h1f) == 8'h05 ||
                              (wdata & 8'h1f) == 8'h06 || (wdata & 8'h1f) == 8'h07 ||
                              (wdata & 8'h1f) == 8'h09 ||
                              (wdata & 8'h1f) == 8'h0a || (wdata & 8'h1f) == 8'h0c ||
                              (wdata & 8'h1f) == 8'h0d || (wdata & 8'h1f) == 8'h0f;
    wire command_wait = data_io && write_cycle && state == IDLE &&
                        (fetch_pending || ((seeking || queued_seek) && read_write_command));
    // After a controller-only reset, retire an already accepted backend read
    // before admitting a new command that could change its address.
    // A software-forced DMA without an active read must retire, rather
    // than hanging the machine waiting for a DRQ that can never arrive.
    wire ready = dma ? (write_cycle || dma_data_ready ||
                        (state != READ_DATA && state != WAIT_MOTOR)) :
                 !command_wait;
    pcxt_isa_endpoint endpoint (
        .clk(clk), .reset(reset), .request(request), .response(response),
        .device_valid(active), .device_request(access),
        .device_claim(dor_io || msr_io || data_io || dma),
        .device_ready(ready), .device_rdata(rdata), .device_commit(commit),
        .irq(irq), .drq(drq)
    );

    wire terminal = access[`FES_PCXT_ISA_TERMINAL_COUNT_BIT];
    // Final CHRN is the position following a complete sector; this is how
    // XT BIOS calculates the number of transferred sectors. At the end of
    // head 1 it advances to head 0 of the next cylinder.
    wire at_eot = read_sector == read_eot;
    wire [7:0] following_sector = at_eot ? 8'd1 : read_sector + 8'd1;
    wire [7:0] following_head = at_eot ? (read_head == 0 && read_mt ? 8'd1 : 8'd0) : read_head;
    wire [7:0] following_cylinder = at_eot && !(read_head == 0 && read_mt)
                                    ? read_cylinder + 8'd1 : read_cylinder;

    task automatic result7(input [7:0] st0, st1, st2, c, h, r, n);
        begin
            result_bytes[0] <= st0; result_bytes[1] <= st1; result_bytes[2] <= st2;
            result_bytes[3] <= c; result_bytes[4] <= h; result_bytes[5] <= r; result_bytes[6] <= n;
            result_index <= 0; result_length <= 7; state <= RESULT;
            result_irq_pending <= 1; data_valid <= 0;
        end
    endtask
    task automatic result1(input [7:0] value);
        begin
            result_bytes[0] <= value;
            result_index <= 0; result_length <= 1; state <= RESULT;
        end
    endtask
    task automatic start_seek(input [7:0] target);
        reg [7:0] distance;
        begin
            distance = cylinder > target ? cylinder - target : target - cylinder;
            if (sense_valid) begin
                // Preserve the preceding unsensed completion. A subsequent
                // seek starts after Sense consumes that status, so IRQ6 can
                // deassert and reassert for the new completion.
                queued_seek <= 1; queued_failed <= 0; queued_target <= target;
            end else begin
                seek_target <= target;
                seek_ticks <= (20'(distance) + 20'd1) * 20'(STEP_TICKS);
                seeking <= 1;
            end
            state <= IDLE;
        end
    endtask

    task automatic fail_seek(input [7:0] st0);
        begin
            if (sense_valid) begin
                queued_seek <= 1; queued_failed <= 1; queued_st0 <= st0;
            end else begin
                sense_st0 <= st0; sense_cylinder <= cylinder;
                sense_valid <= 1; sense_irq_pending <= 1;
            end
            state <= IDLE;
        end
    endtask

    integer i;
    always @(posedge clk) begin
        if (reset || request[`FES_PCXT_ISA_RESET_BIT]) begin
            state <= IDLE; dor <= 0; command <= 0; param_count <= 0; param_total <= 0;
            result_index <= 0; result_length <= 0;
            sense_irq_pending <= 0; result_irq_pending <= 0; sense_valid <= 0;
            sense_st0 <= 0; sense_cylinder <= 0; motor_ticks <= 0;
            seeking <= 0; queued_seek <= 0; queued_failed <= 0; queued_st0 <= 0; queued_target <= 0;
            dma_mode <= 0; cylinder <= 0; seek_target <= 0; seek_ticks <= 0;
            read_cylinder <= 0; read_sector <= 0; read_eot <= 0; read_head <= 0; read_mt <= 0; lost_ready <= 0;
            lba <= 0; byte_index <= 0; byte_ticks <= 0; data_byte <= 0;
            data_valid <= 0; fetch_pending <= 0;
            for (i=0; i<8; i=i+1) params[i] <= 0;
            for (i=0; i<7; i=i+1) result_bytes[i] <= 0;
        end else begin
            if (guest_ce) begin
                if (!motor_on) motor_ticks <= 0;
                else if (!motor_ready) motor_ticks <= motor_ticks + 1'b1;
                if (state == READ_DATA && byte_ticks != 0) byte_ticks <= byte_ticks - 1'b1;
                if (seeking && motor_ready && seek_ticks != 0) begin
                    seek_ticks <= seek_ticks - 1'b1;
                    if (seek_ticks == 1) begin
                        seeking <= 0; cylinder <= seek_target;
                        // Completion status is latched below with priority over
                        // an earlier Sense Interrupt acknowledgement.
                    end
                end
            end
            // A queued seek cannot discard the previous completion. It starts
            // only after its status has been consumed, giving IRQ a fresh edge.
            if (queued_seek && !sense_valid && !seeking) begin
                queued_seek <= 0;
                if (queued_failed || !motor_on || !media_present) begin
                    sense_cylinder <= cylinder; sense_st0 <= queued_failed ? queued_st0 : 8'h48;
                    sense_valid <= 1; sense_irq_pending <= 1;
                end else begin
                    seek_target <= queued_target;
                    if (cylinder > queued_target)
                        seek_ticks <= (20'(cylinder - queued_target) + 20'd1) * 20'(STEP_TICKS);
                    else
                        seek_ticks <= (20'(queued_target - cylinder) + 20'd1) * 20'(STEP_TICKS);
                    seeking <= 1;
                end
            end
            // Once requested, a byte remains outstanding until the storage
            // response. An ejected image cannot cancel an accepted request.
            if (fetch_pending && media_read_ready) begin
                fetch_pending <= 0;
                if (state == READ_DATA && media_present) begin
                    data_byte <= media_read_data; data_valid <= 1;
                end
            end
            if ((state == WAIT_MOTOR || state == READ_DATA) && (!motor_on || !media_present))
                lost_ready <= 1;
            if (state == WAIT_MOTOR) begin
                if (lost_ready || !motor_on || !media_present)
                    result7(8'h48 | {5'b0, read_head[0], 2'b00}, 8'h04, 0,
                            read_cylinder, read_head, read_sector, 8'd2);
                else if (motor_ready) begin
                    state <= READ_DATA; byte_ticks <= BYTE_TICKS;
                end
            end
            if (state == READ_DATA) begin
                // A device byte already being prepared is allowed to commit;
                // the next cycle reports lost readiness. An outstanding media
                // request remains asserted until its response drains.
                if ((lost_ready || !motor_on || !media_present) && !(dma && !write_cycle && data_valid))
                    result7(8'h48 | {5'b0, read_head[0], 2'b00}, 8'h04, 0,
                            read_cylinder, read_head, read_sector, 8'd2);
                else if (media_present && motor_on && !fetch_pending && !data_valid)
                    fetch_pending <= 1;
            end
            if (commit && dor_io && write_cycle) begin
                dor <= wdata;
                if (!wdata[2]) begin
                    state <= IDLE; sense_irq_pending <= 0; result_irq_pending <= 0; sense_valid <= 0;
                    seeking <= 0; queued_seek <= 0; dma_mode <= 0; data_valid <= 0;
                    param_count <= 0; result_index <= 0;
                end else if (!dor[2]) begin
                    // The first Sense Interrupt after reset reports the
                    // drive-0 ready-change condition expected by an XT BIOS.
                    state <= IDLE; sense_st0 <= 8'hc0; sense_cylinder <= cylinder;
                    sense_valid <= 1; sense_irq_pending <= 1;
                end
            end else if (commit && data_io && !write_cycle && state == RESULT) begin
                if (result_index == 0) result_irq_pending <= 0;
                if (result_index + 3'd1 == result_length) begin
                    result_index <= 0; result_length <= 0; state <= IDLE;
                end else result_index <= result_index + 1'b1;
            end else if (commit && data_io && write_cycle && dor[2]) begin
                if (state == IDLE) begin
                    command <= wdata; param_count <= 0;
                    case (wdata & 8'h1f)
                        8'h03: begin param_total <= 2; state <= PARAMS; end // Specify
                        8'h04: begin param_total <= 1; state <= PARAMS; end // Sense drive
                        8'h05, 8'h06, 8'h09, 8'h0c: begin param_total <= 8; state <= PARAMS; end
                        8'h07, 8'h0a: begin param_total <= 1; state <= PARAMS; end
                        8'h0d: begin param_total <= 5; state <= PARAMS; end // Format
                        8'h0f: begin param_total <= 2; state <= PARAMS; end
                        8'h08: begin // Sense Interrupt Status
                            result_bytes[0] <= sense_valid ? sense_st0 : 8'h80;
                            result_bytes[1] <= sense_cylinder;
                            result_index <= 0; result_length <= sense_valid ? 3'd2 : 3'd1;
                            sense_valid <= 0; sense_irq_pending <= 0; state <= RESULT;
                        end
                        default: result1(8'h80);
                    endcase
                end else if (state == PARAMS) begin
                    params[param_count[2:0]] <= wdata;
                    if (param_count + 4'd1 < param_total) param_count <= param_count + 1'b1;
                    else case (command & 8'h1f)
                        // Specify step/head timings are conservatively fixed;
                        // reject non-DMA execution instead of silently emulating it.
                        8'h03: begin dma_mode <= !wdata[0]; state <= IDLE; end
                        8'h04: begin // Sense Drive Status: drive 0, two-sided, TRK0, READY, write-protected
                            result1((wdata & 8'h07) |
                                    ((wdata[1:0] == 0 && dor[1:0] == 0) ?
                                     (8'h48 | (cylinder == 0 ? 8'h10 : 8'h00) |
                                      (motor_ready && media_present ? 8'h20 : 8'h00)) : 8'h00));
                        end
                        8'h07: begin // Recalibrate
                            if (wdata[1:0] == 0 && motor_on && media_present) start_seek(0);
                            else fail_seek(8'h48 | (wdata & 8'h03));
                        end
                        8'h0f: begin // Seek
                            if (params[0][1:0] == 0 && wdata < 40 && motor_on && media_present) start_seek(wdata);
                            else fail_seek(8'h48 | (params[0] & 8'h03));
                        end
                        8'h0a: begin // Read ID on the selected drive
                            if (wdata[1:0] == 0 && media_present && motor_ready)
                                result7(8'h00 | (wdata & 8'h04), 0, 0, cylinder,
                                        {7'b0,wdata[2]}, 8'd1, 8'd2);
                            else result7(8'h48 | (wdata & 8'h07), 8'h04, 0, cylinder,
                                         {7'b0,wdata[2]}, 8'd1, 8'd2);
                        end
                        8'h05, 8'h09: // Write and Write Deleted: immutable medium
                            result7((media_present && motor_on && params[0][1:0] == 0 ? 8'h40 : 8'h48) | (params[0] & 8'h07),
                                    media_present && motor_on && params[0][1:0] == 0 ? 8'h02 : 8'h04, 0,
                                    params[1], params[2], params[3], params[4]);
                        8'h0d: // Format also cannot mutate an immutable image.
                            result7((media_present && motor_on && params[0][1:0] == 0 ? 8'h40 : 8'h48) | (params[0] & 8'h07),
                                    media_present && motor_on && params[0][1:0] == 0 ? 8'h02 : 8'h04, 0, cylinder,
                                    {7'b0,params[0][2]}, 8'd1, params[1]);
                        8'h0c: // Raw sectors contain normal, not deleted, marks.
                            result7((media_present && motor_on && params[0][1:0] == 0 ? 8'h40 : 8'h48) | (params[0] & 8'h07), 8'h04, 8'h40,
                                    params[1], params[2], params[3], params[4]);
                        8'h06: begin
                            read_cylinder <= params[1]; read_head <= params[2];
                            read_sector <= params[3]; read_eot <= params[5]; read_mt <= command[7];
                            if (!media_present || !motor_on || params[0][1:0] != 0 ||
                                params[1] >= 40 || params[1] != cylinder || params[2] >= 2 ||
                                params[2][0] != params[0][2] || params[3] == 0 || params[3] > 9 ||
                                params[4] != 2 || params[5] == 0 || params[5] > 9 ||
                                !command[6] || !dma_mode)
                                result7((media_present && motor_on && params[0][1:0] == 0 ?
                                         8'h40 : 8'h48) | (params[0] & 8'h07), 8'h04, 0,
                                        params[1], params[2], params[3], params[4]);
                            else begin
                                lba <= 10'(params[1]) * 10'd18 + 10'(params[2]) * 10'd9 + 10'(params[3]) - 10'd1;
                                byte_index <= 0; data_valid <= 0; fetch_pending <= 0; lost_ready <= 0;
                                result_irq_pending <= 0; state <= WAIT_MOTOR;
                            end
                        end
                        default: result1(8'h80);
                    endcase
                end
            end else if (commit && dma && write_cycle && (state == READ_DATA || state == WAIT_MOTOR)) begin
                // The medium is immutable; a misdirected DACK ends the
                // in-flight command rather than committing a write.
                result7(8'h40 | {5'b0,read_head[0],2'b00}, 8'h02, 0,
                        read_cylinder, read_head, read_sector, 8'd2);
            end else if (commit && dma && !write_cycle && dma_data_ready) begin
                data_valid <= 0; byte_ticks <= BYTE_TICKS;
                if (lost_ready || !motor_on || !media_present)
                    result7(8'h48 | {5'b0,read_head[0],2'b00}, 8'h04, 0,
                            read_cylinder, read_head, read_sector, 8'd2);
                else if (terminal) begin
                    // The FDC completes the sector internally after TC; no
                    // remaining bytes are delivered over DMA.
                    result7({5'b0,read_head[0],2'b00}, 0, 0,
                            following_cylinder, following_head, following_sector, 8'd2);
                end else if (byte_index == 511) begin
                    if (read_sector == 9 && !at_eot)
                        result7(8'h40 | {5'b0,read_head[0],2'b00}, 8'h04, 0,
                                following_cylinder, following_head, following_sector, 8'd2);
                    else if (at_eot && (!read_mt || read_head != 0))
                        result7(8'h40 | {5'b0,read_head[0],2'b00}, 8'h80, 0,
                                following_cylinder, following_head, following_sector, 8'd2);
                    else begin
                        read_cylinder <= following_cylinder; read_head <= following_head;
                        read_sector <= following_sector;
                        lba <= at_eot ? 10'(read_cylinder) * 10'd18 + 10'd9 : lba + 1'b1;
                        byte_index <= 0;
                    end
                end else byte_index <= byte_index + 1'b1;
            end
            // A newly completed seek wins over an acknowledgement of an
            // earlier pending/no-pending sense on the same fabric edge.
            // The separate result IRQ latch keeps an unread early 0x80 result
            // from clearing a later seek completion.
            if (guest_ce && seeking && motor_ready && seek_ticks == 1 &&
                !(commit && dor_io && write_cycle && !wdata[2])) begin
                sense_cylinder <= seek_target; sense_st0 <= 8'h20;
                sense_valid <= 1; sense_irq_pending <= 1;
            end else if (seeking && (!motor_on || !media_present) &&
                         !(commit && dor_io && write_cycle && !wdata[2])) begin
                seeking <= 0; sense_cylinder <= cylinder; sense_st0 <= 8'h48;
                sense_valid <= 1; sense_irq_pending <= 1;
            end
        end
    end
    wire unused_access = ^{address[19:10], access[47:40], access[36], access[34:31], params[6], params[7], dor[7:5]};
endmodule
