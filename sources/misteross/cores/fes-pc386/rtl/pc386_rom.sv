// SPDX-License-Identifier: MIT
// The pc386 firmware ROM on DDR port 0 (docs/pc386-machine-design.md,
// "Guest physical memory" and "How the package carries them"): the VGA BIOS
// at C0000h-C7FFFh and the system BIOS at F0000h-FFFFFh, 96 KiB of M10K in
// the sealed RBF. It sits between fes_i386_cpu's port 0 and fes_hps_ddr's.
// Until the CPU has an L2 and its memory router, the L1s fill straight from
// port 0, so this answers each read of a ROM range from the M10K and passes
// every other command to the port. The CPU's attribute map makes both ranges
// read-only and maps the reset alias at FFFF0000h onto F0000h itself, so no
// write reaches the ROM and no read names the alias.
//
// A ROM read waits until every read before it has returned from the port,
// then takes the command and returns its beats, one a clock, while every
// later command waits; read data so returns in command order. ROM_HEX holds
// 6144 lines of 128 bits, written by scripts/build_pc386_firmware.py: the VGA
// BIOS's 2048 beats, then the system BIOS's 4096, byte 0 in bits 7:0.
// RAM_BASE[19:4] must be zero.
module pc386_rom #(
    parameter ROM_HEX = "build/firmware/pc386/pc386-rom.hex",
    parameter [31:0] RAM_BASE = 32'h3400_0000  // the HPS address of guest physical 0
) (
    input wire clk, reset,
    // The CPU's port 0
    input wire [27:0] address,
    input wire [7:0] burstcount,
    input wire read, write,
    input wire [127:0] writedata,
    input wire [15:0] byteenable,
    output wire waitrequest,
    output wire [127:0] readdata,
    output wire readdatavalid,
    // fes_hps_ddr's port 0
    output wire [27:0] m_address,
    output wire [7:0] m_burstcount,
    output wire m_read, m_write,
    output wire [127:0] m_writedata,
    output wire [15:0] m_byteenable,
    input wire m_waitrequest,
    input wire [127:0] m_readdata,
    input wire m_readdatavalid,
    output wire idle  // no ROM beat due and no port read outstanding
);
    localparam [11:0] RAM_PAGE = RAM_BASE[31:20];
    wire guest = address[27:16] == RAM_PAGE;
    wire vga_bios = guest && address[15:11] == 5'b11000;  // C0000h-C7FFFh
    wire system_bios = guest && address[15:12] == 4'hf;    // F0000h-FFFFFh
    wire rom_read = read && (vga_bios || system_bios);
    wire [12:0] rom_index = system_bios ? 13'd2048 + {1'b0, address[11:0]} : {2'b00, address[10:0]};

    (* ramstyle = "M10K" *) reg [127:0] contents [0:6143];
    initial $readmemh(ROM_HEX, contents);

    reg [7:0] left;      // beats still to read from the ROM
    reg [12:0] index;
    reg beat;            // q is a beat for the CPU
    reg [127:0] q;
    reg [8:0] pending;   // port read beats not yet returned
    wire busy = left != 8'd0 || beat;
    wire rom_take = rom_read && !busy && pending == 9'd0;

    assign m_address = address;
    assign m_burstcount = burstcount;
    assign m_read = read && !rom_read && !busy;
    assign m_write = write && !busy;
    assign m_writedata = writedata;
    assign m_byteenable = byteenable;
    assign waitrequest = rom_read ? !rom_take : busy || m_waitrequest;
    assign readdata = beat ? q : m_readdata;
    assign readdatavalid = beat || m_readdatavalid;
    assign idle = !busy && pending == 9'd0;

    always @(posedge clk) q <= contents[index];
    always @(posedge clk)
        if (reset) begin
            left <= 0; index <= 0; beat <= 0; pending <= 0;
        end else begin
            beat <= left != 8'd0;
            if (rom_take) begin
                left <= burstcount;
                index <= rom_index;
            end else if (left != 8'd0) begin
                left <= left - 8'd1;
                index <= index + 13'd1;
            end
            pending <= pending + (m_read && !m_waitrequest ? {1'b0, burstcount} : 9'd0) - {8'd0, m_readdatavalid};
        end
endmodule
