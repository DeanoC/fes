// SPDX-License-Identifier: GPL-2.0-or-later
// Native 1541 motherboard: 1 MHz 6502, 2 KiB RAM, 16 KiB user ROM,
// two 6522 slices and the IEC inverter/ATN acknowledge gate. The disk pins
// are explicit; this does not substitute sector replies for drive execution.
// c64_machine connects the disk electronics and independently mapped drive ROM.
module c64_drive1541 #(
    parameter integer SYSTEM_CLOCK_HZ = 52_224_000
) (
    input wire clk, reset,
    input wire atn_low, host_clock_low, host_data_low,
    output wire drive_clock_low, drive_data_low,
    input wire [7:0] disk_data, disk_pb,
    input wire byte_ready_n,
    output wire [7:0] disk_pa_out, disk_pa_ddr, disk_pb_out, disk_pb_ddr,
    output wire byte_ready_enable, read_mode,
    output wire [15:0] debug_pc,
    output wire ref_ce,
    output wire phi,
    output wire [13:0] rom_address,
    input wire [7:0] rom_data
);
    reg [26:0] phase = 0;
    wire [27:0] phase_next = {1'b0,phase} + 28'd16_000_000;
    assign ref_ce = phase_next >= SYSTEM_CLOCK_HZ;
    reg [3:0] cpu_div = 0;
    always @(posedge clk) if (ref_ce) cpu_div <= cpu_div + 4'd1;
    assign phi = ref_ce && cpu_div == 4'd15;
    always @(posedge clk)
        phase <= ref_ce ? 27'(phase_next - SYSTEM_CLOCK_HZ) : phase_next[26:0];
    reg [4:0] reset_hold = 5'd16;
    always @(posedge clk)
        if (reset) reset_hold <= 5'd16;
        else if (phi && reset_hold != 0) reset_hold <= reset_hold - 5'd1;
    wire cpu_reset = reset || reset_hold != 0;
    wire [15:0] ab;
    wire [7:0] data_out;
    wire we, irq1, irq2;
    reg [7:0] ram [0:2047];
    reg [7:0] ram_q;
    wire ram_cs = !bus_addr[15] && bus_addr[12:11] == 2'b00;
    wire via1_cs = !bus_addr[15] && bus_addr[12:10] == 3'b110;
    wire via2_cs = !bus_addr[15] && bus_addr[12:10] == 3'b111;
    wire [7:0] via1_q, via2_q;
    reg [15:0] bus_addr = 0;
    reg [7:0] bus_data = 0, cpu_data = 8'hff;
    reg bus_we = 0;
    reg [5:0] cycle_clock = 0;
    always @(posedge clk) begin
        if (phi) begin
            bus_addr <= ab; bus_data <= data_out; bus_we <= we && !cpu_reset;
            cycle_clock <= 0;
        end else if (cycle_clock != 63) cycle_clock <= cycle_clock + 6'd1;
        if (cycle_clock == 43) cpu_data <= data_in;
    end
    // Sample near the end of the 1 us cycle. VIA read side effects occur on
    // the same edge, after cpu_data takes the pre-acknowledgement port value.
    // An early access would release a latched GCR byte before it is sampled.
    wire bus_access = cycle_clock == 43 && !cpu_reset;
    wire [7:0] data_in = bus_addr[15] ? rom_data : ram_cs ? ram_q :
                               via1_cs ? via1_q : via2_cs ? via2_q : 8'hff;
    // The NMOS SO edge reaches V through two clock phases. Sample the pin
    // at the end of Phi1, then present it at the following Phi1 boundary.
    // Keep the VIA CA1 input on the raw BYTE READY signal: delaying that
    // latch along with CPU overflow would change which byte is captured.
    // This models half-cycle sampling, not the analogue setup-time margin.
    reg sampled_so_n = 1'b1, cpu_so_n = 1'b1;
    always @(posedge clk) begin
        if (cpu_reset) begin
            sampled_so_n <= byte_ready_n;
            cpu_so_n <= byte_ready_n;
        end else if (ref_ce) begin
            if (cpu_div == 4'd7) sampled_so_n <= byte_ready_n;
            if (cpu_div == 4'd0) cpu_so_n <= sampled_so_n;
        end
    end
    cpu6502 #(.ENABLE_SO(1)) cpu (.clk(clk), .reset(cpu_reset), .AB(ab), .DI(cpu_data),
        .DO(data_out), .WE(we), .IRQ(irq1 || irq2), .NMI(1'b0), .RDY(phi), .SO(cpu_so_n));
    assign debug_pc = ab;
    assign rom_address = bus_addr[13:0];
    always @(posedge clk) begin
        ram_q <= ram[bus_addr[10:0]];
        if (bus_access && bus_we && ram_cs) ram[bus_addr[10:0]] <= bus_data;
    end
    wire [7:0] serial_out, serial_ddr, serial_effective;
    wire [7:0] serial_pb = {atn_low,2'b00,2'b11,
                            host_clock_low || drive_clock_low,1'b1,
                            host_data_low || drive_data_low};
    assign serial_effective = (serial_out & serial_ddr) | ~serial_ddr;
    assign drive_clock_low = serial_effective[3];
    assign drive_data_low = serial_effective[1] || (serial_effective[4] ^ atn_low);
    c64_via serial_via (
        .clk(clk), .reset(cpu_reset), .phi(phi), .access(bus_access),
        .cs(via1_cs), .we(bus_we), .rs(bus_addr[3:0]), .din(bus_data), .dout(via1_q),
        .pa_pin(8'hff), .pb_pin(serial_pb), .ca1(atn_low), .ca2(1'b1),
        .cb1(1'b1), .cb2(1'b1), .pa_reg(), .pa_ddr(), .pb_reg(),
        .pb_ddr(serial_ddr), .pb_output(serial_out), .ca2_output(), .cb2_output(),
        .ca2_oe(), .cb2_oe(), .irq(irq1)
    );
    wire [7:0] disk_pb_effective;
    wire ca2_oe, cb2_oe, ca2_out, cb2_out;
    c64_via disk_via (
        .clk(clk), .reset(cpu_reset), .phi(phi), .access(bus_access),
        .cs(via2_cs), .we(bus_we), .rs(bus_addr[3:0]), .din(bus_data), .dout(via2_q),
        .pa_pin((disk_data & ~disk_pa_ddr) | (disk_pa_out & disk_pa_ddr)),
        .pb_pin((disk_pb & ~disk_pb_ddr) | (disk_pb_effective & disk_pb_ddr)),
        .ca1(byte_ready_n), .ca2(1'b1), .cb1(1'b1), .cb2(1'b1),
        .pa_reg(disk_pa_out), .pa_ddr(disk_pa_ddr), .pb_reg(),
        .pb_ddr(disk_pb_ddr), .pb_output(disk_pb_effective),
        .ca2_output(ca2_out), .cb2_output(cb2_out), .ca2_oe(ca2_oe),
        .cb2_oe(cb2_oe), .irq(irq2)
    );
    assign disk_pb_out = (disk_pb_effective & disk_pb_ddr) | ~disk_pb_ddr;
    assign byte_ready_enable = !ca2_oe || ca2_out;
    assign read_mode = !cb2_oe || cb2_out;
endmodule
