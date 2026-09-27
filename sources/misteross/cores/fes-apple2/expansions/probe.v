// SPDX-License-Identifier: GPL-2.0-or-later
// Open FES Apple II probe card for any physical slot socket.
//
// The card is built separately and linked into one socket of the frozen
// shell, so its top-level ports are the socket plugs: FPGA_CLK1_50 is spliced
// onto the shell system clock, plug_addr is the registered request word and
// plug_rdata the response word (apple2_bus.vh).
//
//   $C0n0      scratch register (read/write)
//   $C0n1      card ID $A2
//   $C0n2      DEVSEL access counter
//   $C0n3      tone divider: nonzero plays a square wave into the slot audio
//   $Cn00      256-byte firmware page (diagnostic/probe_card.py)
//   $C800-CFFF 1 KiB RAM (mirrored), enabled by a $Cn00 access and released
//              by any access to $CFFF, as on the original backplane.
`include "apple2_bus.vh"

module cart (
    input  wire FPGA_CLK1_50,
    input  wire [`A2_BUS_REQ-1:0] plug_addr,
    output wire [`A2_BUS_RSP-1:0] plug_rdata
);
    wire clk = FPGA_CLK1_50;
    wire [15:0] addr = plug_addr[`A2_BUS_A];
    wire [7:0] wdata = plug_addr[`A2_BUS_D];
    wire bus_read = plug_addr[`A2_BUS_READ];
    wire strobe = plug_addr[`A2_BUS_STROBE];
    wire bus_reset = plug_addr[`A2_BUS_RESET];
    wire devsel = plug_addr[`A2_BUS_DEVSEL];
    wire iosel = plug_addr[`A2_BUS_IOSEL];
    wire iostrobe = plug_addr[`A2_BUS_IOSTROBE];

    reg [7:0] scratch = 8'd0;
    reg [7:0] counter = 8'd0;
    reg [7:0] tone_div = 8'd0;
    reg c800_owned = 1'b0;
    wire ram_select = iostrobe && c800_owned;

    always @(posedge clk) begin
        if (bus_reset) begin
            scratch <= 8'd0;
            counter <= 8'd0;
            tone_div <= 8'd0;
            c800_owned <= 1'b0;
        end else if (strobe) begin
            if (devsel) begin
                counter <= counter + 8'd1;
                if (!bus_read && addr[3:0] == 4'd0) scratch <= wdata;
                if (!bus_read && addr[3:0] == 4'd3) tone_div <= wdata;
            end
            if (iosel)
                c800_owned <= 1'b1;
            else if (iostrobe && addr[10:0] == 11'h7ff)
                c800_owned <= 1'b0;
        end
    end

    // $C800 RAM: registered read of the held address, one write per cycle.
    reg [7:0] ram [0:1023];
    reg [7:0] ram_q = 8'd0;
    always @(posedge clk) begin
        if (strobe && ram_select && !bus_read)
            ram[addr[9:0]] <= wdata;
        ram_q <= ram[addr[9:0]];
    end

    // $Cn00 page in an explicitly placed-by-nextpnr async-read M10K.
`include "probe_rom.vh"
    wire [7:0] rom_data;
`ifdef VERILATOR
    localparam [(1 << 10) * 10 - 1:0] ROM_WORDS = PROBE_ROM_INIT;
    assign rom_data = ROM_WORDS[{6'd0, addr[7:0]} * 14'd10 +: 8];
`else
    wire [9:0] rom_lane;
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1), .INIT(PROBE_ROM_INIT)) rom (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1ADDR({2'b00, addr[7:0]}), .B1DATA(rom_lane), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    assign rom_data = rom_lane[7:0];
`endif

    // Square-wave tone: half period (tone_div + 1) * 256 system clocks.
    reg [15:0] tone_count = 16'd0;
    reg tone_level = 1'b0;
    always @(posedge clk) begin
        if (bus_reset || tone_div == 8'd0) begin
            tone_count <= 16'd0;
            tone_level <= 1'b0;
        end else if (tone_count == {tone_div, 8'hff}) begin
            tone_count <= 16'd0;
            tone_level <= !tone_level;
        end else begin
            tone_count <= tone_count + 16'd1;
        end
    end
    wire [15:0] audio = tone_div == 8'd0 ? 16'd0 : (tone_level ? 16'h0800 : 16'hf800);

    reg [7:0] read_data;
    reg drive;
    always @* begin
        read_data = 8'h00;
        drive = 1'b0;
        if (bus_read && devsel && addr[3:2] == 2'b00) begin
            drive = 1'b1;
            case (addr[1:0])
                2'd0: read_data = scratch;
                2'd1: read_data = 8'ha2;
                2'd2: read_data = counter;
                default: read_data = tone_div;
            endcase
        end else if (bus_read && iosel) begin
            drive = 1'b1;
            read_data = rom_data;
        end else if (bus_read && ram_select) begin
            drive = 1'b1;
            read_data = ram_q;
        end
    end
    assign plug_rdata = {audio, 1'b0, 1'b0, 1'b0, drive, read_data};
endmodule
