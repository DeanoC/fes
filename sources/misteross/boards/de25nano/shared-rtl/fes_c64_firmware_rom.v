// SPDX-License-Identifier: GPL-2.0-or-later
// Commodore 64 firmware filled at launch (fes.fabric.firmware-upload), in the
// byte order of c64_rom: BASIC 0..8191, KERNAL 8192..16383, character ROM
// 16384..20479 and 1541 ROM 20480..36863. The read ports keep c64_rom's
// latencies: the CPU's data follows its address by two clocks, the VIC's
// character read and the drive's read by one. A CPU address past the
// character ROM reads $FF, as c64_rom's does.
//
// Uploaded byte pairs always start at an even address, so each region is two
// byte lanes with one write port each. The VIC and the CPU both read the
// character ROM, so it has its own copy for the VIC, written with the CPU's.
// The contents power up undefined; the firmware front end keeps the machine
// held until an image is committed. No Commodore ROM bytes are in this
// repository or the package.
module fes_c64_firmware_rom (
    input  wire        clk,
    input  wire [14:0] address,
    output reg  [7:0]  data,
    input  wire [11:0] video_address,
    output wire [7:0]  video_data,
    input  wire [13:0] drive_address,
    output wire [7:0]  drive_data,
    input  wire [15:0] write_addr,
    input  wire [15:0] write_data,
    input  wire [1:0]  write_enable
);
    localparam integer CPU_BYTES = 20480;
    localparam integer CHAR_BASE = 16384;
    localparam integer DRIVE_BASE = 20480;
    // A pair's address is even; its halfword index selects the lane row.
    wire [14:0] pair = write_addr[15:1];
    wire cpu_write = write_addr < 16'(CPU_BYTES);
    wire char_write = write_addr >= 16'(CHAR_BASE) && write_addr < 16'(CPU_BYTES);
    wire drive_write = write_addr >= 16'(DRIVE_BASE) && write_addr < 16'(DRIVE_BASE + 16384);

    // CPU: BASIC, KERNAL and the character ROM, two registered stages.
    (* ramstyle = "M20K" *) reg [7:0] cpu_even [0:CPU_BYTES/2-1];
    (* ramstyle = "M20K" *) reg [7:0] cpu_odd [0:CPU_BYTES/2-1];
    reg [7:0] cpu_even_q, cpu_odd_q;
    reg cpu_lane, cpu_present;
    wire cpu_in_range = address < 15'(CPU_BYTES);
    wire [13:0] cpu_row = cpu_in_range ? address[14:1] : 14'd0;
    always @(posedge clk) begin
        if (cpu_write && write_enable[0]) cpu_even[pair[13:0]] <= write_data[7:0];
        if (cpu_write && write_enable[1]) cpu_odd[pair[13:0]] <= write_data[15:8];
        cpu_even_q <= cpu_even[cpu_row];
        cpu_odd_q <= cpu_odd[cpu_row];
        cpu_lane <= address[0];
        cpu_present <= cpu_in_range;
        data <= !cpu_present ? 8'hff : cpu_lane ? cpu_odd_q : cpu_even_q;
    end

    // VIC: the character ROM's own copy, one registered stage.
    (* ramstyle = "M20K" *) reg [7:0] char_even [0:2047];
    (* ramstyle = "M20K" *) reg [7:0] char_odd [0:2047];
    reg [7:0] char_even_q, char_odd_q;
    reg char_lane;
    always @(posedge clk) begin
        if (char_write && write_enable[0]) char_even[pair[10:0]] <= write_data[7:0];
        if (char_write && write_enable[1]) char_odd[pair[10:0]] <= write_data[15:8];
        char_even_q <= char_even[video_address[11:1]];
        char_odd_q <= char_odd[video_address[11:1]];
        char_lane <= video_address[0];
    end
    assign video_data = char_lane ? char_odd_q : char_even_q;

    // 1541: one registered stage.
    (* ramstyle = "M20K" *) reg [7:0] drive_even [0:8191];
    (* ramstyle = "M20K" *) reg [7:0] drive_odd [0:8191];
    reg [7:0] drive_even_q, drive_odd_q;
    reg drive_lane;
    wire [14:0] drive_pair = pair - 15'(DRIVE_BASE / 2);
    always @(posedge clk) begin
        if (drive_write && write_enable[0]) drive_even[drive_pair[12:0]] <= write_data[7:0];
        if (drive_write && write_enable[1]) drive_odd[drive_pair[12:0]] <= write_data[15:8];
        drive_even_q <= drive_even[drive_address[13:1]];
        drive_odd_q <= drive_odd[drive_address[13:1]];
        drive_lane <= drive_address[0];
    end
    assign drive_data = drive_lane ? drive_odd_q : drive_even_q;
endmodule
