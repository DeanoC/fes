// SPDX-License-Identifier: GPL-3.0-or-later
// Actual native mailbox/writer/arbiter boundary; C++ supplies delayed storage.
module st_media_lifecycle_sim_top (
    input wire clk, cold_reset,
    input wire [31:0] gpo, output wire [31:0] gpi,
    output wire [1:0] unit0_state, output wire [31:0] unit0_size,
    output wire frozen,
    input wire job_req, input wire [23:0] job_ram_addr,
    input wire [19:1] job_media_addr,
    output wire job_ready, job_error, writer_busy, changed,
    output wire dma_req, output wire [23:0] dma_addr,
    input wire dma_ready, input wire [15:0] dma_data,
    output wire writer_req, host_req, output wire [1:0] client_ready,
    output wire memory_req, output wire [19:1] memory_addr,
    output wire [15:0] memory_data, output wire [1:0] memory_enable,
    input wire memory_ready
);
    wire [19:0] host_addr;
    wire [15:0] host_data, host_memory_data, writer_data;
    wire [1:0] host_enable, host_memory_enable, ready;
    wire host_ready;
    wire [19:1] host_memory_addr;
    wire [19:1] writer_addr;
    wire unused_exec_reset, unused_mouse_valid, unused_snapshot_req;
    wire [143:0] unused_keyboard;
    wire [15:0] unused_controllers;
    wire signed [15:0] unused_mouse_dx, unused_mouse_dy;
    wire [1:0] unused_mouse_buttons;
    wire [19:0] unused_snapshot_addr;
    assign client_ready = ready;
    fes_computer_mailbox #(
        .ENABLE_ATARI_ST_FLOPPY(1), .ENABLE_ATARI_ST_FLOPPY_WRITE(1),
        .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(20),
        .UNIT0_MIN(1024), .UNIT0_MAX(1024)
    ) endpoint (
        .clk(clk), .gpo(gpo), .gpi(gpi),
        .build_id(128'h00112233445566778899aabbccddeeff),
        .exec_reset(unused_exec_reset), .keyboard_rows(unused_keyboard),
        .controller_buttons(unused_controllers),
        .mouse_valid(unused_mouse_valid), .mouse_dx(unused_mouse_dx),
        .mouse_dy(unused_mouse_dy), .mouse_buttons(unused_mouse_buttons), .mouse_ready(1'b0),
        .media_write_addr(host_addr), .media_write_data(host_data),
        .media_write_enable(host_enable), .media_write_ready(host_ready),
        .media_write_busy(writer_busy), .media_changed(changed), .media_frozen(frozen),
        .media_read_req(unused_snapshot_req), .media_read_addr(unused_snapshot_addr),
        .media_read_ready(1'b0), .media_read_data(8'd0),
        .unit0_state(unit0_state), .unit0_size(unit0_size)
    );
    st_media_writer upload (
        .clk(clk), .cold_reset(cold_reset), .source_addr(host_addr), .source_data(host_data),
        .source_enable(host_enable), .source_ready(host_ready),
        .memory_req(host_req), .memory_addr(host_memory_addr), .memory_wdata(host_memory_data),
        .memory_byte_enable(host_memory_enable), .memory_ready(ready[0])
    );
    // Native FDC jobs cease when the mailbox stops presenting the old image.
    st_floppy_writer writer (
        .clk(clk), .cold_reset(cold_reset), .frozen(frozen),
        .job_req(job_req && unit0_state == 2'd3),
        .job_ram_addr(job_ram_addr), .job_media_addr(job_media_addr),
        .job_ready(job_ready), .job_error(job_error), .busy(writer_busy), .changed(changed),
        .dma_req(dma_req), .dma_addr(dma_addr), .dma_ready(dma_ready), .dma_data(dma_data),
        .media_req(writer_req), .media_addr(writer_addr), .media_data(writer_data), .media_ready(ready[1])
    );
    st_media_port #(.ADDR_BITS(19)) writes (
        .clk(clk), .cold_reset(cold_reset), .source_req({writer_req, host_req}),
        .source_addr0(host_memory_addr), .source_addr1(writer_addr),
        .source_data0(host_memory_data), .source_data1(writer_data),
        .source_enable0(host_memory_enable), .source_enable1(2'b11), .source_ready(ready),
        .memory_req(memory_req), .memory_addr(memory_addr),
        .memory_data(memory_data), .memory_enable(memory_enable), .memory_ready(memory_ready)
    );
endmodule
