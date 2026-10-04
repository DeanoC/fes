// SPDX-License-Identifier: GPL-3.0-or-later
// Sector-atomic RAM -> .st backend. The owner holds job_req and addresses
// until job_ready. A withdrawn collection changes no disk byte. Once all
// 256 words have been collected, the commit drains even if ownership/reset
// withdraws job_req; its canceled completion is never returned to a new job.
// Only cold_reset resets this backend. Freeze fences new jobs, not commits.
module st_floppy_writer (
    input wire clk, cold_reset, frozen,
    input wire job_req,
    input wire [23:0] job_ram_addr,
    input wire [19:1] job_media_addr,
    output reg job_ready,
    output reg job_error,
    output wire busy,
    output reg changed,
    output wire dma_req,
    output wire [23:0] dma_addr,
    input wire dma_ready,
    input wire [15:0] dma_data,
    output wire media_req,
    output wire [19:1] media_addr,
    output wire [15:0] media_data,
    input wire media_ready
);
    typedef enum logic [2:0] { IDLE, COLLECT, RAM_GAP, COMMIT, DISK_GAP, DONE } state_t;
    state_t state;
    reg [15:0] sector [0:255];
    reg [23:0] ram_base;
    reg [19:1] disk_base;
    reg [7:0] word_index;
    reg abandoned;
    reg [15:0] commit_word;
    assign busy = state != IDLE && state != DONE;
    assign dma_req = state == COLLECT && job_req && !cold_reset;
    assign dma_addr = ram_base + {15'd0, word_index, 1'b0};
    assign media_req = state == COMMIT && !cold_reset;
    assign media_addr = disk_base + {11'd0, word_index};
    assign media_data = commit_word;
    // Unreset synchronous read: load the first word at the collection edge,
    // and each following word in the mandatory disk request gap. No memory
    // contents are observable until the entire sector has been collected.
    always @(posedge clk) begin
        if (state == COLLECT && word_index == 255 && dma_ready && job_req)
            commit_word <= sector[0];
        else if (state == DISK_GAP) commit_word <= sector[word_index];
    end
    always @(posedge clk) begin
        job_ready <= 1'b0;
        changed <= 1'b0;
        if (cold_reset) begin
            state <= IDLE;
            ram_base <= 0;
            disk_base <= 0;
            word_index <= 0;
            abandoned <= 0;
            job_error <= 0;
        end else begin
            if (!job_req && (state == COMMIT || state == DISK_GAP)) abandoned <= 1;
            case (state)
                IDLE: if (job_req && !frozen) begin
                    ram_base <= job_ram_addr;
                    disk_base <= job_media_addr;
                    word_index <= 0;
                    abandoned <= 0;
                    job_error <= 0;
                    if (job_ram_addr[0] || job_ram_addr < 24'd8 ||
                        job_ram_addr > 24'h07fe00 || job_media_addr > 19'd368384) begin
                        job_error <= 1;
                        job_ready <= 1;
                        state <= DONE;
                    end else state <= COLLECT;
                end
                COLLECT: begin
                    if (!job_req) state <= DONE;
                    else if (dma_ready) begin
                        sector[word_index] <= dma_data;
                        if (word_index == 255) begin
                            word_index <= 0;
                            state <= COMMIT;
                        end else begin
                            word_index <= word_index + 1'b1;
                            state <= RAM_GAP;
                        end
                    end
                end
                RAM_GAP: state <= job_req ? COLLECT : DONE;
                COMMIT: if (media_ready) begin
                    if (word_index == 255) begin
                        changed <= 1;
                        if (job_req && !abandoned) job_ready <= 1;
                        state <= DONE;
                    end else begin
                        word_index <= word_index + 1'b1;
                        state <= DISK_GAP;
                    end
                end
                DISK_GAP: state <= COMMIT;
                DONE: if (!job_req) state <= IDLE;
                default: state <= IDLE;
            endcase
        end
    end
endmodule
