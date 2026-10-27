// SPDX-License-Identifier: MIT
// AT interrupt controllers (docs/pc386-machine-design.md, "What carries over"):
// two unmodified KF8259 imports, the master at 20h and the slave at A0h, the
// slave's INT on master IR2. Both see every INTA pulse; the master names the
// slave on the cascade lines at the first pulse, and whichever chip owns the
// interrupt drives the vector on the second.
module pc386_pic (
    input wire clk, reset,
    input wire master_cs, slave_cs,
    input wire read_strobe, write_strobe, inta_strobe,
    input wire a0,
    input wire [7:0] wdata,
    output wire [7:0] rdata,     // the selected chip's register, or the vector
    input wire [15:0] irq,       // IRQ2 is the cascade and is not an input
    output wire intr
);
    wire [7:0] master_data, slave_data;
    wire master_floating, slave_floating, slave_intr;
    wire [2:0] cascade;
    wire rd_n = !read_strobe, wr_n = !write_strobe, inta_n = !inta_strobe;
    // During INTA exactly one chip drives the bus; a floating bus reads FFh.
    assign rdata = !master_floating ? master_data : !slave_floating ? slave_data : 8'hff;

    /* verilator lint_off PINCONNECTEMPTY */
    KF8259 master (
        .clock(clk), .reset(reset),
        .chip_select_n(!master_cs), .read_enable_n(rd_n), .write_enable_n(wr_n), .address(a0),
        .data_bus_in(wdata), .data_bus_out(master_data), .data_bus_io(master_floating),
        .cascade_in(3'b000), .cascade_out(cascade), .cascade_io(),
        .slave_program_n(1'b1), .buffer_enable(), .slave_program_or_enable_buffer(),
        .interrupt_acknowledge_n(inta_n), .interrupt_to_cpu(intr),
        .interrupt_request({irq[7:3], slave_intr, irq[1:0]})
    );
    KF8259 slave (
        .clock(clk), .reset(reset),
        .chip_select_n(!slave_cs), .read_enable_n(rd_n), .write_enable_n(wr_n), .address(a0),
        .data_bus_in(wdata), .data_bus_out(slave_data), .data_bus_io(slave_floating),
        .cascade_in(cascade), .cascade_out(), .cascade_io(),
        .slave_program_n(1'b0), .buffer_enable(), .slave_program_or_enable_buffer(),
        .interrupt_acknowledge_n(inta_n), .interrupt_to_cpu(slave_intr),
        .interrupt_request(irq[15:8])
    );
    /* verilator lint_on PINCONNECTEMPTY */
    wire unused = irq[2];
endmodule
