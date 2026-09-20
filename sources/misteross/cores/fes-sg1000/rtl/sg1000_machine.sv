// SPDX-License-Identifier: GPL-2.0-or-later
// Reduced SG-1000 machine for the FES simple-computer Quartus bring-up.
//
// This is a Coleco sibling, not a second console stack: TV80, the bounded
// TMS9918-style VDP, dual-port RAM wrappers and the 16 KiB mailbox blob are
// the Coleco modules. The SG-1000 first slice only replaces the memory map
// and the 8255 joystick ports. There is no BIOS shim; the cartridge occupies
// 0x0000. Audio, mappers, 32/48 KiB retail images and SC-3000 keyboard
// hardware remain outside this slice.

`ifdef FES_SG1000_OSS
`define FES_SG1000_REGISTERED_MEDIA
`elsif QUARTUS
`define FES_SG1000_REGISTERED_MEDIA
`endif

module sg1000_machine (
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
    output wire [7:0]  port_dc,
    output wire [7:0]  port_dd,
    output wire [7:0]  logical_x,
    output wire [7:0]  logical_y,
    output wire [3:0]  logical_pixel,
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
    // copied code or let the VDP run until the final write.
    wire      machine_reset = reset || !media_ready || !media_loaded;
`ifdef FES_SG1000_REGISTERED_MEDIA
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
    wire [3:0] vdp_raster_pixel;
    wire       vdp_raster_blank;
    wire       vdp_status_collision;
    wire       vdp_status_overflow;
    wire [4:0] vdp_status_fifth_index;
    wire       vdp_irq_n;
    reg        vdp_write_seen;
    wire       vdp_bus_ce = ce_cpu_n && (nWR || !vdp_write_seen);

    // TV80 holds an OUT bus cycle across more than one negative CPU enable.
    // The VDP consumes a byte per strobe, so acknowledge a held write once.
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

    // SG-1000 / SMS-style 8255 Port A/B. Keyboard bits 0..9 keep the Coleco
    // first-slice matrix: P1 Up/Right/Down/Left/Fire1, P2 the same. Active-low.
    assign port_dc = {keyboard[8], keyboard[7], keyboard[5], keyboard[4],
                      keyboard[1], keyboard[3], keyboard[2], keyboard[0]};
    assign port_dd = {6'b111111, keyboard[9], keyboard[6]};
    assign controller1_value = {3'b111, keyboard[4], keyboard[1],
                                keyboard[3], keyboard[2], keyboard[0]};
    assign controller2_value = {3'b111, keyboard[9], keyboard[6],
                                keyboard[8], keyboard[7], keyboard[5]};

    initial begin
        media_addr = 14'h0000;
        media_loaded = 1'b0;
        reset_d = 1'b0;
        vdp_write_seen = 1'b0;
`ifdef FES_SG1000_REGISTERED_MEDIA
        media_data_valid = 1'b0;
        media_request_done = 1'b0;
        media_write_addr = 14'h0000;
`endif
        ce_cpu_p = 1'b0;
        ce_cpu_n = 1'b0;
        ce_vdp = 1'b0;
        ce_counter = 5'h00;
    end

    // Same 52 MHz enable shape as the Coleco first slice, approximating the
    // 3.58 MHz CPU/VDP cadence.
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
    wire cpu_ram_select = cpu_addr[15:14] == 2'b11;
    wire cpu_cartridge_select = cpu_addr[15:14] == 2'b00;
    wire peek_ram_select = peek_addr[15:14] == 2'b11;
    wire peek_cartridge_select = peek_addr[15:14] == 2'b00;
    wire ppi_select = cpu_addr[7:2] == 6'b110111;

`ifdef FES_SG1000_REGISTERED_MEDIA
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

    reg [7:0] io_read_data;
    always @* begin
        io_read_data = 8'hff;
        if (cpu_addr[7:0] == 8'hbe || cpu_addr[7:0] == 8'hbf)
            io_read_data = vdp_cpu_dout;
        else if (ppi_select)
            io_read_data = cpu_addr[0] ? port_dd : port_dc;
    end

    always @* begin
        cpu_din = 8'hff;
        if (cpu_mem_read) begin
            if (cpu_cartridge_select)
                cpu_din = cartridge_read;
            else if (cpu_ram_select)
                cpu_din = ram_read;
        end else if (cpu_io_read) begin
            cpu_din = io_read_data;
        end
    end

    always @(posedge clk_sys) begin
        reset_d <= reset;
        if (!media_ready || (reset && !reset_d)) begin
            media_addr <= 14'h0000;
            media_loaded <= 1'b0;
`ifdef FES_SG1000_REGISTERED_MEDIA
            media_data_valid <= 1'b0;
            media_request_done <= 1'b0;
            media_write_addr <= 14'h0000;
`endif
        end else if (!media_loaded && media_size != 15'd0) begin
`ifdef FES_SG1000_REGISTERED_MEDIA
            if (!media_data_valid) begin
                media_data_valid <= 1'b1;
                media_write_addr <= 14'h0000;
                if (media_size == 15'd1)
                    media_request_done <= 1'b1;
                else
                    media_addr <= 14'h0001;
            end else if (media_request_done) begin
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
        if (peek_cartridge_select)
            peek_data = cartridge_peek;
        else if (peek_ram_select)
            peek_data = ram_peek;
    end
endmodule

`ifdef FES_SG1000_REGISTERED_MEDIA
`undef FES_SG1000_REGISTERED_MEDIA
`endif
