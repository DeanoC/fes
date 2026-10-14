// SPDX-License-Identifier: GPL-3.0-or-later
// One 52.224 MHz SDRAM port shared fairly by CPU, video, DMA and media.
// Requests/data must remain stable until ready. Each held request completes
// once, with a one-clock ready pulse, and rearms only after req is low.
// Physical words 00000-3ffff hold 512 KiB ST RAM; 40000-a67ff hold the
// separate floppy image (up to 820 KiB). The controller's packed row/bank/column
// address mapping is preserved: these are physical halfword offsets.
// cold_reset restarts SDRAM initialization. Warm reset drains any physical
// transaction, suppresses any remaining completion, and keeps refresh/memory
// intact. An already-acknowledged posted write commits its retained payload.
module st_memory #(
    // At 52.224 MHz the controller's four-count wait leaves six chip clocks
    // (114.9 ns) between runtime refresh and the next command. The ISSI
    // IS42S16320D requires 60 ns; pad/route acceptance remains separate.
    parameter [13:0] REFRESH_WAIT_CYCLES = 14'd4,
    parameter EARLY_COMPLETION = 1,
    // Retain the older extra input stage for timing comparisons. Production
    // consumes the DDR rising word directly at the matching capture count.
    parameter REGISTERED_READ_INPUT = 0,
    parameter PHASE_SLOTS = 0,
    parameter [1:0] CPU_SLOT_PHASE = 2'd0,
    parameter SINGLE_RANK_REFRESH = 0,
    parameter SLOT_REFRESH = 0,
    // Retain and acknowledge one CPU write before its physical commit.
    parameter POSTED_CPU_WRITES = 0
) (
    input wire clk,
    input wire [1:0] memory_phase,
    input wire raster_reset,
    input wire clk_pin,
    input wire cold_reset,
    input wire reset,
    output wire initialized,
    input wire cpu_req,
    input wire [18:1] cpu_addr,
    input wire cpu_write,
    input wire [15:0] cpu_wdata,
    input wire [1:0] cpu_byte_enable,
    output wire cpu_ready,
    output wire [15:0] cpu_rdata,
    input wire video_req,
    input wire [18:1] video_addr,
    output reg video_ready,
    output reg [15:0] video_rdata,
    input wire dma_req,
    input wire [23:0] dma_addr,
    input wire dma_write,
    input wire [15:0] dma_wdata,
    input wire [1:0] dma_byte_enable,
    output reg dma_ready,
    output reg [15:0] dma_rdata,
    input wire media_write_req,
    input wire [19:1] media_write_addr,
    input wire [15:0] media_write_wdata,
    input wire [1:0] media_write_byte_enable,
    output reg media_write_ready,
    input wire media_read_req,
    input wire [19:0] media_read_addr,
    output reg media_read_ready,
    output reg [7:0] media_read_rdata,
    output wire sdram_clk,
    output wire sdram_cke,
    output wire sdram_ncs,
    output wire sdram_nras,
    output wire sdram_ncas,
    output wire sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml,
    output wire sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_rise,
    input wire [15:0] dq_fall
);
    generate
        if (SLOT_REFRESH && (!PHASE_SLOTS || !SINGLE_RANK_REFRESH)) begin : invalid_refresh_policy
            initial $fatal(1, "slot refresh requires phase slots and single-rank refresh");
        end
        if (POSTED_CPU_WRITES && (!EARLY_COMPLETION || !PHASE_SLOTS)) begin : invalid_posted_policy
            initial $fatal(1, "posted writes require early completion and phase slots");
        end
    endgenerate

    localparam [2:0] CPU = 3'd0, VIDEO = 3'd1, DMA = 3'd2,
                     MEDIA_WRITE = 3'd3, MEDIA_READ = 3'd4;
    typedef enum logic [1:0] { IDLE, BUSY, REARM, EMPTY } state_t;
    state_t state;
    reg [2:0] cursor, owner;
    reg [4:0] seen;
    wire [4:0] requests = {media_read_req, media_write_req, dma_req, video_req, cpu_req};
    reg write_video_turn;
    reg grant;
    reg advance_cursor, earlier_nonvideo_pending;
    reg [2:0] selected;
    reg selected_valid, selected_write;
    reg [25:0] selected_addr;
    reg [15:0] selected_wdata;
    reg [1:0] selected_byte_enable;
    reg [25:0] held_addr;
    reg held_write, held_odd, discard;
    reg [15:0] held_wdata;
    reg [1:0] held_byte_enable;
    wire controller_done;
    wire refresh_pending;
    wire [15:0] controller_rdata;
    reg cpu_ready_q;
    reg [15:0] cpu_rdata_q;
    // Present a valid grant to an idle controller on the arbitration edge.
    // Once launched, BUSY retains the latched command until completion; a
    // controller still draining its previous recovery accepts it later.
    wire launch = EARLY_COMPLETION && state == IDLE && initialized &&
                  !cold_reset && !reset && grant && selected_valid;
    // Capture has already finished before controller_done asserts. The CPU
    // may consume that word at the next fabric edge; other clients retain
    // their registered boundary. Latch it on that edge for held-request data.
    wire cpu_completion = state == BUSY && owner == CPU && controller_done &&
                          !cold_reset && !reset && !discard && cpu_req &&
                          !(POSTED_CPU_WRITES && held_write);
    assign cpu_ready = EARLY_COMPLETION ?
        (cpu_completion || (POSTED_CPU_WRITES && held_write && cpu_ready_q &&
                            !cold_reset && !reset && cpu_req)) : cpu_ready_q;
    assign cpu_rdata = EARLY_COMPLETION && cpu_completion ? controller_rdata : cpu_rdata_q;
    // The optional legacy stage and its later controller capture refer to
    // the same physical DDR rising sample as the direct production path.
    reg [15:0] dq_rise_q, dq_fall_q;
    always @(posedge clk) begin
        dq_rise_q <= dq_rise;
        dq_fall_q <= dq_fall;
    end

    sdram_addon_port #(.BYTE_MASK_ENABLED(1),
                       .REFRESH_WAIT_CYCLES(REFRESH_WAIT_CYCLES),
                       .EARLY_DONE(EARLY_COMPLETION),
                       .RATE0_INPUT_REGISTER(REGISTERED_READ_INPUT),
                       .SINGLE_RANK_REFRESH(SINGLE_RANK_REFRESH),
                       .EXTERNAL_REFRESH_WINDOW(SLOT_REFRESH),
                       .REFRESH_INTERVAL_CYCLES(SLOT_REFRESH ? 12'd335 : 12'd0)) controller (
        .clk(clk), .clk_pin(clk_pin),
        .refresh_window(!SLOT_REFRESH || reset || raster_reset || memory_phase == (CPU_SLOT_PHASE + 2'd2)),
        .refresh_pending(refresh_pending), .rate(2'd0), .reset(cold_reset),
        .start(state == BUSY || launch),
        .write(launch ? selected_write : held_write),
        .addr(launch ? selected_addr : held_addr),
        .wdata(launch ? selected_wdata : held_wdata),
        .write_byte_enable(launch ? selected_byte_enable : held_byte_enable),
        .initialized(initialized), .done(controller_done), .rdata(controller_rdata),
        .sdram_clk(sdram_clk), .sdram_cke(sdram_cke), .sdram_ncs(sdram_ncs),
        .sdram_nras(sdram_nras), .sdram_ncas(sdram_ncas), .sdram_nwe(sdram_nwe),
        .sdram_ba(sdram_ba), .sdram_a(sdram_a), .sdram_dqml(sdram_dqml),
        .sdram_dqmh(sdram_dqmh), .dq_out(dq_out), .dq_oe(dq_oe),
        .dq_rise(REGISTERED_READ_INPUT ? dq_rise_q : dq_rise), .dq_fall(dq_fall_q)
    );

    integer priority_index, candidate;
    always @* begin
        grant = 1'b0;
        advance_cursor = 1'b0;
        earlier_nonvideo_pending = 1'b0;
        selected = CPU;
        candidate = 0;
        for (priority_index = 0; priority_index < 5; priority_index = priority_index + 1) begin
            candidate = int'(cursor) + priority_index;
            if (candidate >= 5) candidate = candidate - 5;
            if (!grant && requests[candidate] && !seen[candidate] &&
                (!SLOT_REFRESH || !refresh_pending || candidate == int'(CPU)) &&
                (!PHASE_SLOTS || candidate != int'(CPU) || !cpu_write ||
                 !video_req || seen[VIDEO] || !write_video_turn) &&
                (candidate == int'(VIDEO) || !PHASE_SLOTS || memory_phase ==
                    (CPU_SLOT_PHASE + ((candidate == int'(CPU) && cpu_write) ? 2'd1 : 2'd0))) &&
                (candidate != int'(VIDEO) || (PHASE_SLOTS ?
                    memory_phase == (CPU_SLOT_PHASE + 2'd2) : 1'b1))) begin
                grant = 1'b1;
                selected = 3'(candidate);
                // A phase-1 write may bypass an older phase-0 client. Keep
                // that client's queue position until its own grant occurs.
                advance_cursor = candidate != int'(VIDEO) && !earlier_nonvideo_pending;
            end
            if (candidate != int'(VIDEO) && requests[candidate] && !seen[candidate])
                earlier_nonvideo_pending = 1'b1;
        end
        selected_addr = 26'd0;
        selected_write = 1'b0;
        selected_wdata = 16'd0;
        selected_byte_enable = 2'b11;
        selected_valid = 1'b1;
        case (selected)
            CPU: begin
                selected_addr = {8'd0, cpu_addr};
                selected_write = cpu_write;
                selected_wdata = cpu_wdata;
                selected_byte_enable = cpu_byte_enable;
            end
            VIDEO: selected_addr = {8'd0, video_addr};
            DMA: begin
                selected_addr = {3'd0, dma_addr[23:1]};
                selected_write = dma_write;
                selected_wdata = dma_wdata;
                selected_byte_enable = dma_byte_enable;
                selected_valid = dma_addr >= 24'd8 && dma_addr < 24'h080000;
            end
            MEDIA_WRITE: begin
                selected_addr = 26'h040000 + {7'd0, media_write_addr};
                selected_write = 1'b1;
                selected_wdata = media_write_wdata;
                selected_byte_enable = media_write_byte_enable;
                selected_valid = media_write_addr < 19'd419840;
            end
            MEDIA_READ: begin
                selected_addr = 26'h040000 + {7'd0, media_read_addr[19:1]};
                selected_valid = media_read_addr < 20'd839680;
            end
            default: selected_valid = 1'b0;
        endcase
    end

    always @(posedge clk) begin
        cpu_ready_q <= 1'b0;
        video_ready <= 1'b0;
        dma_ready <= 1'b0;
        media_write_ready <= 1'b0;
        media_read_ready <= 1'b0;
        if (cold_reset) begin
            state <= IDLE;
            cursor <= CPU;
            owner <= CPU;
            seen <= 5'd0;
            write_video_turn <= 1'b0;
            held_addr <= 26'd0;
            held_write <= 1'b0;
            held_wdata <= 16'd0;
            held_byte_enable <= 2'd0;
            held_odd <= 1'b0;
            discard <= 1'b0;
            cpu_rdata_q <= 16'd0;
            video_rdata <= 16'd0;
            dma_rdata <= 16'd0;
            media_read_rdata <= 8'd0;
        end else begin
            seen <= seen & requests;
            if (reset) begin
                seen <= requests;
                cursor <= CPU;
                write_video_turn <= 1'b0;
                discard <= 1'b1;
            end
            case (state)
                IDLE: begin
                    if (initialized && !reset && grant) begin
                        owner <= selected;
                        if (selected == CPU && selected_write) write_video_turn <= 1'b1;
                        if (selected == VIDEO) write_video_turn <= 1'b0;
                        if (POSTED_CPU_WRITES && selected == CPU && selected_write && selected_valid)
                            cpu_ready_q <= 1'b1;
                        if (!PHASE_SLOTS || advance_cursor)
                            cursor <= selected == MEDIA_READ ? CPU : selected + 3'd1;
                        seen[selected] <= 1'b1;
                        held_addr <= selected_addr;
                        held_write <= selected_write;
                        held_wdata <= selected_wdata;
                        held_byte_enable <= selected_byte_enable;
                        held_odd <= media_read_addr[0];
                        discard <= 1'b0;
                        state <= selected_valid ? BUSY : EMPTY;
                    end
                end
                BUSY, EMPTY: begin
                    // A reset or force-interrupt may withdraw just one client.
                    // Drain its issued command, but never acknowledge a later
                    // request with the old completion. Other clients continue.
                    if (!requests[owner]) discard <= 1'b1;
                    if (state == EMPTY || controller_done) begin
                        // Suppress completions for requests invalidated by warm
                        // reset. The already-issued SDRAM command still drains.
                        if (!reset && !discard && requests[owner]) case (owner)
                            CPU: begin
                                cpu_ready_q <= state == EMPTY || !POSTED_CPU_WRITES || !held_write;
                                cpu_rdata_q <= state == EMPTY ? 16'd0 : controller_rdata;
                            end
                            VIDEO: begin
                                video_ready <= 1'b1;
                                video_rdata <= state == EMPTY ? 16'd0 : controller_rdata;
                            end
                            DMA: begin
                                dma_ready <= 1'b1;
                                dma_rdata <= state == EMPTY ? 16'd0 : controller_rdata;
                            end
                            MEDIA_WRITE: media_write_ready <= 1'b1;
                            MEDIA_READ: begin
                                media_read_ready <= 1'b1;
                                media_read_rdata <= state == EMPTY ? 8'd0 :
                                    held_odd ? controller_rdata[7:0] : controller_rdata[15:8];
                            end
                            default: ;
                        endcase
                        state <= REARM;
                    end
                end
                REARM: state <= IDLE;
                default: state <= IDLE;
            endcase
        end
    end
endmodule
