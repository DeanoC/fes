// The real ZX81 CPU/ULA and RAM socket, with an independently built pack's
// logical interface. This is behavioral acceptance, not CRAM-link evidence.
module expansion_machine #(
    parameter PACK_PRESENT = 0
) (
    input wire clk_sys, reset,
    input wire [39:0] keyboard,
    input wire tape_ready,
    input wire [14:0] tape_size,
    input wire [7:0] tape_data,
    output wire [13:0] tape_addr_out,
    output wire ce_6m5, video_pixel, hblank, vblank, hsync_out, vsync_out, halt_n,
    output wire [15:0] cpu_addr,
    input wire [15:0] peek_addr,
    output wire [7:0] peek_data
);
    wire [13:0] address, pack_address, pack_peek_address;
    wire [7:0] write_data, read_data, pack_write_data, pack_data, pack_peek_data;
    wire write_enable, pack_write_enable;
    wire pack_present;
    zx81_machine #(.EXTERNAL_RAM(1)) machine (
        .clk_sys(clk_sys), .reset(reset), .keyboard(keyboard),
        .tape_ready(tape_ready), .tape_size(tape_size), .tape_data(tape_data),
        .tape_addr_out(tape_addr_out), .ce_6m5(ce_6m5), .video_pixel(video_pixel),
        .hblank(hblank), .vblank(vblank), .hsync_out(hsync_out), .vsync_out(vsync_out),
        .halt_n(halt_n), .cpu_addr(cpu_addr), .peek_addr(peek_addr), .peek_data(),
        .ram_address(address), .ram_write_data(write_data), .ram_write_enable(write_enable),
        .external_ram_data(read_data), .external_peek_data(peek_data)
    );
    zx81_ram_socket socket (
        .clock(clk_sys), .address(address), .write_data(write_data), .write_enable(write_enable),
        .peek_address(peek_addr[13:0]), .read_data(read_data), .peek_data(peek_data),
        .pack_present(PACK_PRESENT != 0 && pack_present), .pack_data(pack_data), .pack_peek_data(pack_peek_data),
        .pack_address(pack_address), .pack_write_data(pack_write_data),
        .pack_write_enable(pack_write_enable), .pack_peek_address(pack_peek_address)
    );
    cart pack (
        .FPGA_CLK1_50(clk_sys),
        .plug_addr({pack_peek_address, pack_write_enable, pack_write_data, pack_address}),
        .plug_rdata({pack_present, pack_peek_data, pack_data})
    );
endmodule
