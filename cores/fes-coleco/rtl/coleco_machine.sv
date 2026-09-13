// SPDX-License-Identifier: GPL-2.0-or-later
// Reduced ColecoVision machine for the FES simple-computer bringup.
//
// This is intentionally an adapter-sized machine rather than a claim of full
// ColecoVision compatibility: the reset shim replaces the proprietary BIOS,
// the cartridge aperture is a single 16 KiB blob, and the VDP exposes the
// bounded Graphics I slice implemented in coleco_vdp.sv.

`ifdef FES_COLECO_OSS
`define FES_COLECO_REGISTERED_MEDIA
`elsif QUARTUS
`define FES_COLECO_REGISTERED_MEDIA
`endif

module coleco_machine (
    input  wire        clk_sys,
    input  wire        reset,
    input  wire [39:0] keyboard,
    input  wire        media_ready,
    input  wire [14:0] media_size,
    input  wire [7:0]  media_data,
    output reg  [13:0] media_addr,
    input  wire [15:0] peek_addr,
    output reg  [7:0]  peek_data,
    output wire [7:0]  controller1_value,
    output wire [7:0]  controller2_value,
    output wire [7:0]  logical_x,
    output wire [7:0]  logical_y,
    output wire [1:0]  logical_pixel,
    output wire        logical_blank,
    output wire [7:0]  vdp_status,
    output wire [15:0] cpu_addr_debug,
    output wire        cpu_halt_n
);
    localparam [13:0] CARTRIDGE_LAST = 14'h3fff;

    reg       media_loaded;
    reg       reset_d;
    // MEDIA_COMMIT acknowledges the mailbox, not the subsequent cartridge
    // copy. An immediate host RELEASE must not let the CPU fetch partly
    // copied code or let the VDP run until the final write (OSS flush included).
    wire      machine_reset = reset || !media_ready || !media_loaded;
`ifdef FES_COLECO_REGISTERED_MEDIA
    reg       media_data_valid;
    reg       media_request_done;
    reg [13:0] media_write_addr;
`endif

    wire [15:0] cpu_addr;
    wire [7:0] cpu_dout;
    reg  [7:0] cpu_din;
    wire nM1;
    wire nMREQ;
    wire nIORQ;
    wire nRD;
    wire nWR;
    wire nRFSH;
    wire nHALT;

    reg ce_cpu_p;
    reg ce_cpu_n;
    reg ce_vdp;
    reg [4:0] ce_counter;

    wire [7:0] vdp_cpu_dout;
    wire [8:0] vdp_raster_y;
    wire [7:0] vdp_raster_x;
    wire [1:0] vdp_raster_pixel;
    wire       vdp_raster_blank;
    wire       vdp_status_collision;
    wire       vdp_status_overflow;
    wire [4:0] vdp_status_fifth_index;
    wire       vdp_irq_n;
    reg        vdp_write_seen;
    wire       vdp_bus_ce = ce_cpu_n && (nWR || !vdp_write_seen);

    // TV80 holds an OUT bus cycle across more than one negative CPU enable.
    // The VDP consumes a byte per strobe, so acknowledge a held write once.
    // Reads retain the existing sampling window for the CPU data input.
    always @(posedge clk_sys) begin
        if (machine_reset || nIORQ || nWR)
            vdp_write_seen <= 1'b0;
        else if (vdp_bus_ce)
            vdp_write_seen <= 1'b1;
    end

    assign cpu_addr_debug = cpu_addr;
    assign cpu_halt_n = nHALT;
    assign logical_x = vdp_raster_x;
    assign logical_y = vdp_raster_y[7:0];
    assign logical_pixel = vdp_raster_pixel;
    assign logical_blank = vdp_raster_blank;
    assign vdp_status = {vdp_raster_blank, vdp_status_overflow,
                         vdp_status_collision, vdp_status_fifth_index};

    // Coleco's common latch selects keypad (80..9F) or joystick (C0..DF).
    // Repeated clocks during one held OUT are harmless: this is a set/reset
    // latch, not a toggle, and the output byte is ignored.
    reg controller_joystick;
    always @(posedge clk_sys) begin
        if (machine_reset)
            controller_joystick <= 1'b0;
        else if (ce_cpu_n && !nIORQ && !nWR) begin
            case (cpu_addr[7:5])
                3'b100: controller_joystick <= 1'b0;
                3'b110: controller_joystick <= 1'b1;
                default: ;
            endcase
        end
    end

    // CPU data-bit encoding, including cv_ctrl's pin permutation. Reference:
    // MiSTer ColecoVision 5e8713cbc91b7d7abe4806cb87834b16d7348011.
    // Lowest key index wins (0..9, *, #), matching the reference's priority.
    function [3:0] keypad_code;
        input [11:0] keys;
        begin
            if      (!keys[0])  keypad_code = 4'ha;
            else if (!keys[1])  keypad_code = 4'hd;
            else if (!keys[2])  keypad_code = 4'h7;
            else if (!keys[3])  keypad_code = 4'hc;
            else if (!keys[4])  keypad_code = 4'h2;
            else if (!keys[5])  keypad_code = 4'h3;
            else if (!keys[6])  keypad_code = 4'he;
            else if (!keys[7])  keypad_code = 4'h5;
            else if (!keys[8])  keypad_code = 4'h1;
            else if (!keys[9])  keypad_code = 4'hb;
            else if (!keys[10]) keypad_code = 4'h9;
            else if (!keys[11]) keypad_code = 4'h6;
            else               keypad_code = 4'hf;
        end
    endfunction

    // D7=0, D5/D4=1 for stationary standard controllers; no quadrature input.
    // Existing first two rows retain directions/fire1; spare matrix bits carry
    // fire2 and the two keypads. The FES keyboard wire contract is unchanged.
    assign controller1_value = {1'b0, controller_joystick ? keyboard[4] : keyboard[10],
                                2'b11, controller_joystick ? keyboard[3:0] :
                                keypad_code(keyboard[23:12])};
    assign controller2_value = {1'b0, controller_joystick ? keyboard[9] : keyboard[11],
                                2'b11, controller_joystick ? keyboard[8:5] :
                                keypad_code(keyboard[35:24])};

    initial begin
        media_addr = 14'h0000;
        media_loaded = 1'b0;
        reset_d = 1'b0;
        vdp_write_seen = 1'b0;
        controller_joystick = 1'b0;
`ifdef FES_COLECO_REGISTERED_MEDIA
        media_data_valid = 1'b0;
        media_request_done = 1'b0;
        media_write_addr = 14'h0000;
`endif
        ce_cpu_p = 1'b0;
        ce_cpu_n = 1'b0;
        ce_vdp = 1'b0;
        ce_counter = 5'h00;
    end

    // The reference machine runs from 52 MHz. These pulses retain the
    // proven ZX81 TV80 half-cycle shape while approximating the Coleco 3.58
    // MHz CPU/VDP cadence for the first OSS route.
    always @(negedge clk_sys) begin
        ce_counter <= ce_counter + 1'b1;
        ce_cpu_p <= !ce_counter[3] && !ce_counter[2:0];
        ce_cpu_n <= ce_counter[3] && !ce_counter[2:0];
        ce_vdp <= !ce_counter[3:0];
    end

    T80pa cpu (
        .RESET_n(~machine_reset),
        .CLK(clk_sys),
        .CEN_p(ce_cpu_p),
        .CEN_n(ce_cpu_n),
        .WAIT_n(1'b1),
        .INT_n(1'b1),
        .NMI_n(vdp_irq_n),
        .BUSRQ_n(1'b1),
        .M1_n(nM1),
        .MREQ_n(nMREQ),
        .IORQ_n(nIORQ),
        .RD_n(nRD),
        .WR_n(nWR),
        .RFSH_n(nRFSH),
        .HALT_n(nHALT),
        .BUSAK_n(),
        .A(cpu_addr),
        .DO(cpu_dout),
        .DI(cpu_din)
    );

    coleco_vdp vdp (
        .clk(clk_sys),
        .reset(machine_reset),
        .cpu_ce(vdp_bus_ce),
        .cpu_iorq_n(nIORQ),
        .cpu_rd_n(nRD),
        .cpu_wr_n(nWR),
        .cpu_a(cpu_addr[7:0]),
        .cpu_din(cpu_dout),
        .cpu_dout(vdp_cpu_dout),
        .raster_ce(ce_vdp),
        .raster_x(vdp_raster_x),
        .raster_y(vdp_raster_y),
        .raster_pixel(vdp_raster_pixel),
        .raster_blank(vdp_raster_blank),
        .status_collision(vdp_status_collision),
        .status_overflow(vdp_status_overflow),
        .status_fifth_index(vdp_status_fifth_index),
        .irq_n(vdp_irq_n)
    );

    wire cpu_mem_read = !nMREQ && !nRD;
    wire cpu_io_read = !nIORQ && !nRD;
    wire cpu_mem_write = !nMREQ && !nWR;
    wire cpu_ram_select = cpu_addr[15:13] == 3'b011;
    wire cpu_cartridge_select = cpu_addr[15:14] == 2'b10 ||
                                 cpu_addr[15:14] == 2'b11;

    // Keep the three machine memories in the same explicit wrapper shape as
    // the ZX81 bringup. The OSS mapper cannot reliably infer the larger
    // direct arrays with -nolutram; the wrapper selects a true dual-port M10K
    // implementation under FES_COLECO_OSS. Quartus altsyncram also registers
    // its read addresses, even with UNREGISTERED outputs; only the default
    // simulation branch is asynchronous.
`ifdef FES_COLECO_REGISTERED_MEDIA
    // Both compiler RAMs have a one-clock read result. media_addr requests
    // the next byte from the GP mailbox, while media_write_addr identifies the
    // byte currently present on media_data. The final request is held for one
    // extra clock so the last registered result is written as well.
    wire media_load_write = !media_loaded && media_ready && media_data_valid;
    wire [13:0] cartridge_address_a = media_load_write ? media_write_addr :
                                      cpu_addr[13:0];
    wire cartridge_wren_a = media_load_write;
`else
    wire media_load_active = !media_loaded && media_ready && media_size != 15'd0;
    wire [13:0] cartridge_address_a = media_load_active ? media_addr :
                                      cpu_addr[13:0];
    wire cartridge_wren_a = media_load_active;
`endif
    wire [7:0] cartridge_read;
    wire [7:0] cartridge_peek;
    wire [7:0] ram_read;
    wire [7:0] ram_peek;
    wire [7:0] reset_rom_read;
    wire [7:0] reset_rom_peek;

    coleco_dpram #(
        .ADDRWIDTH(14),
        .NUMWORDS(16384)
    ) cartridge_ram (
        .clock(clk_sys),
        .address_a(cartridge_address_a),
        .data_a(media_data),
        .wren_a(cartridge_wren_a),
        .q_a(cartridge_read),
        .address_b(peek_addr[13:0]),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(cartridge_peek)
    );

    coleco_dpram #(
        .ADDRWIDTH(10),
        .NUMWORDS(1024)
    ) cpu_ram_block (
        .clock(clk_sys),
        .address_a(cpu_addr[9:0]),
        .data_a(cpu_dout),
        .wren_a(cpu_mem_write && cpu_ram_select),
        .q_a(ram_read),
        .address_b(peek_addr[9:0]),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(ram_peek)
    );

`ifdef QUARTUS
    localparam RESET_ROM_INIT = "cores/fes-coleco/rtl/coleco_reset_rom.mif";
`else
    localparam RESET_ROM_INIT = "cores/fes-coleco/rtl/coleco_reset_rom.hex";
`endif

    coleco_dpram #(
        .ADDRWIDTH(13),
        .NUMWORDS(8192),
        .MEM_INIT_FILE(RESET_ROM_INIT)
    ) reset_rom_block (
        .clock(clk_sys),
        .address_a(cpu_addr[12:0]),
        .data_a(8'h00),
        .wren_a(1'b0),
        .q_a(reset_rom_read),
        .address_b(peek_addr[12:0]),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(reset_rom_peek)
    );

    reg [7:0] io_read_data;
    always @* begin
        io_read_data = 8'hff;
        if (cpu_addr[7:0] == 8'hbe || cpu_addr[7:0] == 8'hbf)
            io_read_data = vdp_cpu_dout;
        else if (cpu_addr[7:5] == 3'b111)
            io_read_data = cpu_addr[1] ? controller2_value : controller1_value;
    end

    always @* begin
        cpu_din = 8'hff;
        if (cpu_mem_read) begin
            if (cpu_addr[15:13] == 3'b000)
                cpu_din = reset_rom_read;
            else if (cpu_ram_select)
                cpu_din = ram_read;
            else if (cpu_cartridge_select)
                cpu_din = cartridge_read;
        end else if (cpu_io_read) begin
            cpu_din = io_read_data;
        end
    end

    // Load the committed mailbox blob while machine_reset holds the CPU/VDP,
    // even if host execution reset is already released. Dropped media_ready
    // starts a fresh transaction; reset's rising edge also permits a load when the
    // producer keeps the committed blob asserted.
    always @(posedge clk_sys) begin
        reset_d <= reset;
        if (!media_ready || (reset && !reset_d)) begin
            media_addr <= 14'h0000;
            media_loaded <= 1'b0;
`ifdef FES_COLECO_REGISTERED_MEDIA
            media_data_valid <= 1'b0;
            media_request_done <= 1'b0;
            media_write_addr <= 14'h0000;
`endif
        end else if (!media_loaded && media_size != 15'd0) begin
`ifdef FES_COLECO_REGISTERED_MEDIA
            if (!media_data_valid) begin
                // Prime the registered GP mailbox read. No cartridge write is
                // enabled until media_data contains byte zero.
                media_data_valid <= 1'b1;
                media_write_addr <= 14'h0000;
                if (media_size == 15'd1)
                    media_request_done <= 1'b1;
                else
                    media_addr <= 14'h0001;
            end else if (media_request_done) begin
                // The write enable is still high for this edge, so the final
                // registered byte is committed before marking the load done.
                media_loaded <= 1'b1;
                media_data_valid <= 1'b0;
            end else begin
                media_write_addr <= media_write_addr + 1'b1;
                if (media_addr == CARTRIDGE_LAST ||
                    ({1'b0, media_addr} + 15'd1 >= media_size)) begin
                    media_request_done <= 1'b1;
                end else begin
                    media_addr <= media_addr + 1'b1;
                end
            end
`else
            if (media_addr == CARTRIDGE_LAST ||
                ({1'b0, media_addr} + 15'd1 >= media_size)) begin
                media_loaded <= 1'b1;
            end else begin
                media_addr <= media_addr + 1'b1;
            end
`endif
        end
    end

    always @* begin
        peek_data = 8'hff;
        if (peek_addr[15:13] == 3'b000)
            peek_data = reset_rom_peek;
        else if (peek_addr[15:13] == 3'b011)
            peek_data = ram_peek;
        else if (peek_addr[15:14] == 2'b10 || peek_addr[15:14] == 2'b11)
            peek_data = cartridge_peek;
    end
endmodule

`ifdef FES_COLECO_REGISTERED_MEDIA
`undef FES_COLECO_REGISTERED_MEDIA
`endif
