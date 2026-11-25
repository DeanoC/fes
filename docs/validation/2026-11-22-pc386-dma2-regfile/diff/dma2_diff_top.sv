// fes_dma8237 as pc386_at_core wires controller 2 (no DREQ, every cycle answered the
// next clock with no data), beside pc386_dma2_registers on the same register traffic.
module dma2_diff_top (
    input wire clk, reset, guest_ce, reg_read, reg_write,
    input wire [3:0] reg_address,
    input wire [7:0] reg_wdata,
    output wire [7:0] full_rdata, regs_rdata,
    output wire full_hold
);
    reg full_done;
    wire full_valid;
    always @(posedge clk) full_done <= full_valid;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_dma8237 #(.ADDRESS_BITS(24), .AT_PAGES(1), .REFRESH(0), .WORD_ADDRESS(1)) full (
        .clk(clk), .reset(reset), .guest_ce(guest_ce), .reg_read(reg_read), .reg_write(reg_write),
        .page_write(1'b0), .reg_address(reg_address), .reg_wdata(reg_wdata), .reg_rdata(full_rdata),
        .drq(3'b000), .refresh_clock(1'b0), .hold_request(full_hold), .granted(full_hold),
        .cpu_waiting(1'b0), .cpu_done(1'b0), .cycle_valid(full_valid), .cycle_ready(1'b1), .cycle_done(full_done),
        .cycle_address(), .cycle_wdata(), .cycle_kind(), .cycle_channel(), .cycle_write(), .cycle_tc(),
        .cycle_rdata(8'hff), .cycle_fault(2'b00), .fault(), .refresh_done());
    /* verilator lint_on PINCONNECTEMPTY */
    pc386_dma2_registers regs (.clk(clk), .reset(reset), .reg_read(reg_read), .reg_write(reg_write),
        .reg_address(reg_address), .reg_wdata(reg_wdata), .reg_rdata(regs_rdata));
endmodule
