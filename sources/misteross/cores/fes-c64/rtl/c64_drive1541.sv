// SPDX-License-Identifier: GPL-2.0-or-later
// Development 1541 motherboard: 1 MHz 6502, 2 KiB RAM, 16 KiB user ROM,
// two 6522 slices and the IEC inverter/ATN acknowledge gate. The disk pins
// are explicit; this does not substitute sector replies for drive execution.
// Not selected by the package until the disk electronics/ROM map are ready.
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
    output wire phi
);
    reg [26:0] phase = 0;
    wire [27:0] phase_next = {1'b0,phase} + 28'd1_000_000;
    assign phi = phase_next >= SYSTEM_CLOCK_HZ;
    always @(posedge clk)
        phase <= phi ? 27'(phase_next - SYSTEM_CLOCK_HZ) : phase_next[26:0];
    reg [4:0] reset_hold = 5'd16;
    always @(posedge clk)
        if (reset) reset_hold <= 5'd16;
        else if (phi && reset_hold != 0) reset_hold <= reset_hold - 5'd1;
    wire cpu_reset = reset || reset_hold != 0;
    wire [15:0] ab;
    wire [7:0] data_out;
    wire we, irq1, irq2;
    reg [7:0] ram [0:2047];
    reg [7:0] rom [0:16383];
    reg [7:0] ram_q, rom_q;
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
        if (cycle_clock == 16) cpu_data <= data_in;
    end
    wire bus_access = cycle_clock == 0 && !cpu_reset;
    wire [7:0] data_in = bus_addr[15] ? rom_q : ram_cs ? ram_q :
                               via1_cs ? via1_q : via2_cs ? via2_q : 8'hff;
    cpu6502 cpu (.clk(clk), .reset(cpu_reset), .AB(ab), .DI(cpu_data),
        .DO(data_out), .WE(we), .IRQ(irq1 || irq2), .NMI(1'b0), .RDY(phi));
    assign debug_pc = ab;
    always @(posedge clk) begin
        ram_q <= ram[bus_addr[10:0]];
        rom_q <= rom[bus_addr[13:0]];
        if (bus_access && bus_we && ram_cs) ram[bus_addr[10:0]] <= bus_data;
    end
`ifdef VERILATOR
    string rom_path;
    initial begin
        if (!$value$plusargs("C64_DRIVE_ROM_HEX=%s", rom_path))
            $fatal(1, "1541 development simulation requires an external 16 KiB ROM");
        $readmemh(rom_path, rom);
    end
`endif
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
