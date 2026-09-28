// SPDX-License-Identifier: GPL-2.0-or-later
// FES Commodore 64 motherboard in the 52.224 MHz system domain.
// The 6510 is the shared NMOS 6502 plus the processor port. Phi2 is a
// fractional 1.022727 MHz enable. Video is scanned in the HDMI domain.
`include "c64_bus.vh"
`include "c64_font.vh"

module c64_machine #(
    parameter integer SYSTEM_CLOCK_HZ = 52_224_000,
    parameter integer CPU_CLOCK_HZ = 1_022_727
) (
    input  wire        clk_sys,
    input  wire        reset,
    input  wire [143:0] keyboard_rows,
    input  wire [15:0] controller_buttons,
    input  wire [17:0] media_write_addr,
    input  wire [15:0] media_write_data,
    input  wire [1:0]  media_write_enable,
    input  wire        disk_ready,
    output wire [`C64_BUS_REQ-1:0] slot1_request,
    output wire [`C64_BUS_REQ-1:0] slot2_request,
    input  wire [`C64_BUS_RSP-1:0] slot1_response,
    input  wire [`C64_BUS_RSP-1:0] slot2_response,
    input  wire        video_clk,
    output wire [7:0]  red,
    output wire [7:0]  green,
    output wire [7:0]  blue,
    output wire        de,
    output wire        hsync,
    output wire        vsync,
    output wire signed [15:0] audio_sample,
    output reg  [7:0]  debug_stage,
    output reg  [7:0]  debug_error,
    output wire [7:0]  debug_iec,
    output wire [15:0] debug_pc
);
    localparam integer PHASE_BITS = 27;
    reg [PHASE_BITS-1:0] cpu_phase = 0;
    wire [PHASE_BITS:0] cpu_phase_next = {1'b0, cpu_phase} + CPU_CLOCK_HZ;
    wire cpu_ce = cpu_phase_next >= SYSTEM_CLOCK_HZ;
    always @(posedge clk_sys)
        cpu_phase <= cpu_ce ? PHASE_BITS'(cpu_phase_next - SYSTEM_CLOCK_HZ)
                            : cpu_phase_next[PHASE_BITS-1:0];
    reg [5:0] cycle_clock = 0;
    always @(posedge clk_sys)
        if (cpu_ce) cycle_clock <= 6'd0;
        else if (cycle_clock != 6'd63) cycle_clock <= cycle_clock + 6'd1;
    wire bus_strobe = cycle_clock == 6'd0;
    wire bus_sample = cycle_clock == 6'd16;

    reg [3:0] reset_hold = 4'hf;
    always @(posedge clk_sys)
        if (reset) reset_hold <= 4'hf;
        else if (cpu_ce && reset_hold != 4'd0) reset_hold <= reset_hold - 4'd1;
    wire system_reset = reset || reset_hold != 4'd0;
    reg [1:0] reset_writes = 2'd3;
    always @(posedge clk_sys)
        if (system_reset) reset_writes <= 2'd3;
        else if (cpu_ce && reset_writes != 2'd0) reset_writes <= reset_writes - 2'd1;

    wire [15:0] cpu_ab;
    wire [7:0] cpu_do;
    wire cpu_we;
    reg [7:0] cpu_di = 8'hff;
    wire irq_n, nmi_n;
    cpu6502 cpu (
        .clk(clk_sys), .reset(system_reset), .AB(cpu_ab), .DI(cpu_di),
        .DO(cpu_do), .WE(cpu_we), .IRQ(irq_n), .NMI(nmi_n), .RDY(cpu_ce)
    );
    assign debug_pc = cpu_ab;

    reg [15:0] bus_addr = 16'h0000;
    reg [7:0] bus_wdata = 8'h00;
    reg bus_we = 1'b0;
    always @(posedge clk_sys)
        if (cpu_ce) begin
            bus_addr <= cpu_ab;
            bus_wdata <= cpu_do;
            bus_we <= cpu_we && reset_writes == 2'd0 && !system_reset;
        end

    reg [7:0] ddr = 8'h00;
    reg [7:0] pr = 8'h00;
    wire [7:0] port_eff = {
        ddr[7] ? pr[7] : 1'b1, ddr[6] ? pr[6] : 1'b1,
        ddr[5] ? pr[5] : 1'b1, ddr[4] ? pr[4] : 1'b1,
        ddr[3] ? pr[3] : 1'b1, ddr[2] ? pr[2] : 1'b1,
        ddr[1] ? pr[1] : 1'b1, ddr[0] ? pr[0] : 1'b1
    };
    wire loram = port_eff[0];
    wire hiram = port_eff[1];
    wire charen = port_eff[2];

    wire exrom = slot1_response[`C64_BUS_EXROM] | slot2_response[`C64_BUS_EXROM];
    wire game = slot1_response[`C64_BUS_GAME] | slot2_response[`C64_BUS_GAME];
    wire ultimax = game && !exrom;
    wire cart16 = game && exrom;
    wire cart8 = !game && exrom;
    wire cart_irq = slot1_response[`C64_BUS_IRQ] | slot2_response[`C64_BUS_IRQ];
    wire cart_nmi = slot1_response[`C64_BUS_NMI] | slot2_response[`C64_BUS_NMI];
    wire [7:0] cart_data = slot1_response[`C64_BUS_DRIVE] ? slot1_response[`C64_BUS_RD]
                                                          : slot2_response[`C64_BUS_RD];
    wire cart_hit = slot1_response[`C64_BUS_DRIVE] | slot2_response[`C64_BUS_DRIVE];

    wire a000 = bus_addr[15:13] == 3'b101;
    wire e000 = bus_addr[15:13] == 3'b111;
    wire roml = bus_addr[15:13] == 3'b100;
    wire d000 = bus_addr[15:12] == 4'hD;
    wire show_io = d000 && (ultimax || (!ultimax && charen && (loram || hiram)));
    wire show_char = d000 && !ultimax && !show_io && (loram || hiram);
    wire show_basic = a000 && hiram && loram && !cart16 && !ultimax;
    wire show_kernal = e000 && hiram && !ultimax;
    wire sel_vic = show_io && bus_addr[11:10] == 2'b00;
    wire sel_sid = show_io && bus_addr[11:10] == 2'b01;
    wire sel_color = show_io && bus_addr[11:10] == 2'b10;
    wire sel_cia1 = show_io && bus_addr[15:8] == 8'hDC;
    wire sel_cia2 = show_io && bus_addr[15:8] == 8'hDD;
    wire sel_port = bus_addr[15:1] == 15'h0000;
    wire ram_we = bus_strobe && bus_we && !show_io && !sel_port;

    reg [`C64_BUS_REQ-1:0] request = 0;
    always @(posedge clk_sys) begin
        if (bus_strobe) begin
            request[`C64_BUS_A] <= bus_addr;
            request[`C64_BUS_D] <= bus_wdata;
            request[`C64_BUS_READ] <= ~bus_we;
            request[`C64_BUS_STROBE] <= 1'b1;
            request[`C64_BUS_RESET] <= system_reset;
            request[`C64_BUS_ROML] <= roml && (cart8 || cart16 || ultimax);
            request[`C64_BUS_ROMH] <= (cart16 && a000) || (ultimax && e000);
            request[`C64_BUS_IO1] <= show_io && bus_addr[15:8] == 8'hDE;
            request[`C64_BUS_IO2] <= show_io && bus_addr[15:8] == 8'hDF;
            request[`C64_BUS_BA] <= 1'b1;
        end else
            request[`C64_BUS_STROBE] <= 1'b0;
        if (bus_strobe && bus_we && bus_addr == 16'h0000) ddr <= bus_wdata;
        if (bus_strobe && bus_we && bus_addr == 16'h0001) pr <= bus_wdata;
        if (reset) begin
            ddr <= 8'h00;
            pr <= 8'h00;
            debug_stage <= 8'h00;
            debug_error <= 8'h00;
        end else if (bus_strobe && bus_we && bus_addr == 16'hC000) begin
            debug_stage <= bus_wdata;
        end else if (bus_strobe && bus_we && bus_addr == 16'hC001)
            debug_error <= bus_wdata;
    end

    function automatic [`C64_BUS_REQ-1:0] rom_socket;
        input [`C64_BUS_REQ-1:0] req;
        begin
            rom_socket = req;
            rom_socket[`C64_BUS_IO1] = 1'b0;
            rom_socket[`C64_BUS_IO2] = 1'b0;
        end
    endfunction
    function automatic [`C64_BUS_REQ-1:0] io_socket;
        input [`C64_BUS_REQ-1:0] req;
        begin
            io_socket = req;
            io_socket[`C64_BUS_ROML] = 1'b0;
            io_socket[`C64_BUS_ROMH] = 1'b0;
        end
    endfunction
    assign slot1_request = rom_socket(request);
    assign slot2_request = io_socket(request);

    wire [7:0] ram_q, rom_q;
    wire [15:0] video_ram_addr;
    wire [7:0] video_ram_q;
    c64_ram main_ram (
        .clk_a(clk_sys), .addr_a(bus_addr), .wdata_a(bus_wdata), .we_a(ram_we), .q_a(ram_q),
        .clk_b(video_clk), .addr_b(video_ram_addr), .q_b(video_ram_q)
    );
    wire basic_window = bus_addr[15:13] == 3'b101;
    wire [13:0] rom_address = basic_window ? {1'b0, bus_addr[12:0]} : {1'b1, bus_addr[12:0]};
    c64_rom firmware (
        .clk(clk_sys), .address(rom_address), .data(rom_q)
    );

    wire [7:0] cia1_pa, cia1_pb, cia1_ddra, cia1_ddrb, cia1_dout;
    wire [7:0] cia2_pa, cia2_pb, cia2_ddra, cia2_ddrb, cia2_dout;
    wire cia1_irq, cia2_irq;
    wire [4:0] joy2 = {controller_buttons[4], controller_buttons[3], controller_buttons[2],
                       controller_buttons[1], controller_buttons[0]};
    wire [4:0] joy1 = {controller_buttons[12], controller_buttons[11], controller_buttons[10],
                       controller_buttons[9], controller_buttons[8]};
    wire [4:0] pa1_low_lo = (cia1_ddra[4:0] & ~cia1_pa[4:0]) | (~cia1_ddra[4:0] & joy2);
    wire [2:0] pa1_low_hi = cia1_ddra[7:5] & ~cia1_pa[7:5];
    wire [7:0] pa1_low = {pa1_low_hi, pa1_low_lo};
    wire [7:0] key_rows;
    c64_keyboard keyboard (
        .rows(keyboard_rows), .columns_low(pa1_low), .rows_low(key_rows)
    );
    wire [7:0] pb1_low = (cia1_ddrb & ~cia1_pb) | (~cia1_ddrb & key_rows) |
                         (~cia1_ddrb & {3'b000, joy1});
    c64_cia cia1 (
        .clk(clk_sys), .phi(cpu_ce), .reset(system_reset), .cs(sel_cia1),
        .we(bus_strobe && bus_we && sel_cia1), .rs(bus_addr[3:0]), .din(bus_wdata),
        .dout(cia1_dout), .pa_pin(~pa1_low), .pb_pin(~pb1_low),
        .pa_reg(cia1_pa), .pb_reg(cia1_pb), .pa_ddr(cia1_ddra), .pb_ddr(cia1_ddrb),
        .irq(cia1_irq)
    );

    wire dev_clk_low, dev_data_low;
    wire cpu_clk_low = cia2_ddra[4] & cia2_pa[4];
    wire cpu_data_low = cia2_ddra[5] & cia2_pa[5];
    wire [17:0] disk_addr;
    wire [7:0] disk_byte;
    c64_disk_store disk (
        .clk(clk_sys), .write_addr(media_write_addr), .write_data(media_write_data),
        .write_enable(media_write_enable), .read_addr(disk_addr), .read_data(disk_byte)
    );
    c64_iec iec (
        .clk(clk_sys), .phi(cpu_ce), .reset(system_reset),
        .atn_low(cia2_ddra[3] & cia2_pa[3]),
        .cpu_clk_low(cpu_clk_low), .cpu_data_low(cpu_data_low), .disk_ready(disk_ready),
        .dev_clk_low(dev_clk_low), .dev_data_low(dev_data_low),
        .disk_addr(disk_addr), .disk_byte(disk_byte), .debug(debug_iec)
    );
    wire [7:0] pa2_pin = {(cpu_data_low | dev_data_low), (cpu_clk_low | dev_clk_low),
                          (cia2_ddra[5:0] & cia2_pa[5:0]) | (~cia2_ddra[5:0] & 6'h3F)};
    c64_cia cia2 (
        .clk(clk_sys), .phi(cpu_ce), .reset(system_reset), .cs(sel_cia2),
        .we(bus_strobe && bus_we && sel_cia2), .rs(bus_addr[3:0]), .din(bus_wdata),
        .dout(cia2_dout), .pa_pin(pa2_pin), .pb_pin(8'hFF),
        .pa_reg(cia2_pa), .pb_reg(cia2_pb), .pa_ddr(cia2_ddra), .pb_ddr(cia2_ddrb),
        .irq(cia2_irq)
    );

    wire [7:0] vic_dout, color_dout;
    wire signed [15:0] sid_sample;
    c64_vic vic (
        .clk_sys(clk_sys), .phi(cpu_ce), .pixel_clk(video_clk), .reset(system_reset),
        .cs(sel_vic), .we(bus_strobe && bus_we && sel_vic), .addr(bus_addr[5:0]),
        .din(bus_wdata), .dout(vic_dout),
        .color_cs(sel_color), .color_addr(bus_addr[9:0]), .color_din(bus_wdata),
        .color_we(bus_strobe && bus_we && sel_color), .color_dout(color_dout),
        .bank(~cia2_pa[1:0]), .ram_addr(video_ram_addr), .ram_data(video_ram_q),
        .red(red), .green(green), .blue(blue), .de(de), .hsync(hsync), .vsync(vsync),
        .raster()
    );
    c64_sid sid (
        .clk(clk_sys), .phi(cpu_ce), .reset(system_reset), .cs(sel_sid),
        .we(bus_strobe && bus_we && sel_sid), .addr(bus_addr[4:0]), .din(bus_wdata),
        .sample(sid_sample)
    );
    assign audio_sample = sid_sample;
    assign irq_n = cart_irq | cia1_irq | cia2_irq;
    assign nmi_n = cart_nmi;

    wire [7:0] char_byte = c64_glyph(bus_addr[10:3], bus_addr[2:0]);
    wire [7:0] read_data =
        bus_addr == 16'h0000 ? ddr :
        bus_addr == 16'h0001 ? port_eff :
        sel_vic ? vic_dout :
        sel_sid ? 8'h00 :
        sel_color ? color_dout :
        sel_cia1 ? cia1_dout :
        sel_cia2 ? cia2_dout :
        cart_hit ? cart_data :
        show_char ? char_byte :
        (show_basic || show_kernal) ? rom_q :
        (ultimax && !roml && !e000 && !d000 && bus_addr >= 16'h1000) ? 8'hFF :
        ram_q;

    always @(posedge clk_sys)
        if (bus_sample) cpu_di <= read_data;
endmodule
