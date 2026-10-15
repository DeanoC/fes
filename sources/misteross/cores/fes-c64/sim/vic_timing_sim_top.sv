// SPDX-License-Identifier: MIT
// Exercise native phase preparation and the private framebuffer transaction.
module vic_timing_sim_top (
    input wire clk_sys, pixel_clk, reset, phi,
    input wire [5:0] cycle_clock, addr,
    input wire we, reading, read_sample,
    input wire [7:0] din,
    input wire [1:0] bank,
    input wire mem_we,
    input wire [15:0] mem_addr,
    input wire [7:0] mem_data,
    input wire color_we,
    input wire [9:0] color_addr,
    input wire [7:0] color_data,
    output wire [8:0] raster,
    output wire [5:0] native_cycle,
    output wire dot_tick, prep_tick, frame_done, write_pending,
    output wire [2:0] dot,
    output wire [8:0] prepared_x,
    output wire [7:0] prepared_pointer, prepared_row,
    output wire prepared_pending,
    output wire [3:0] saved_ink,
    output reg [31:0] checked_clocks=0, checked_pixels=0, checked_writes=0,
    output reg [31:0] publications=0, collision_sys_samples=0, displayed_pixels=0
);
    reg [7:0] memory [0:65535];
    reg [7:0] ram_data;
    wire [15:0] ram_addr;
    wire de;
    always @(posedge clk_sys) begin
        if(mem_we) memory[mem_addr] <= mem_data;
        ram_data <= memory[ram_addr];
    end
    c64_vic dut (
        .clk_sys(clk_sys),.phi(phi),.cycle_clock(cycle_clock),.cpu_bus_data(8'ha5),
        .ba_n(),.pixel_clk(pixel_clk),.reset(reset),.cs(1'b1),
        .we(we),.reading(reading),.read_sample(read_sample),.addr(addr),.din(din),
        .dout(),.color_cs(1'b1),.color_addr(color_addr),
        .color_din(color_data),.color_we(color_we),.color_dout(),
        .bank(bank),.ram_addr(ram_addr),.ram_data(ram_data),
        .red(),.green(),.blue(),.de(de),.hsync(),.vsync(),.raster(raster),.irq()
    );
    assign native_cycle=dut.native_cycle;
    assign dot_tick=dut.pixel_strobe;
    assign prep_tick=dut.pixel_prepare;
    assign dot=dut.subpixel;
    assign prepared_x=dut.sprite_dot_x_q;
    assign prepared_pointer=dut.pixel_sprites.pointer_cycle;
    assign prepared_row=dut.pixel_sprites.row_cycle;
    assign prepared_pending=dut.pixel_sprites.pending_update;
    assign frame_done=dut.native_frame_start&&dut.frame_started;
    assign write_pending=dut.frame_write_valid;
    assign saved_ink=dut.frame_write_ink;

    // Save original dot/address/direct color together. D020/D021 are late
    // source; the resolved packet still reaches memory one SYS later.
    reg expected_valid;
    reg [16:0] expected_address;
    reg [3:0] expected_ink;
    reg [2:0] pending_valid,pending_background0,pending_border;
    reg [16:0] pending_address[0:2];
    reg [3:0] pending_ink[0:2];
    always @(posedge clk_sys) begin
        if(reset)begin
            pending_valid<=0;pending_background0<=0;pending_border<=0;
            for(integer stage=0;stage<3;stage=stage+1)begin
                pending_address[stage]<=0;pending_ink[stage]<=0;
            end
            expected_valid<=0; expected_address<=0; expected_ink<=0;
        end else begin
            checked_clocks<=checked_clocks+1;
            if(dut.frame_store.write_enable!=expected_valid)
                $fatal(1,"frame write valid is not exactly one SYS delayed");
            if(expected_valid)begin
                if({dut.frame_store.write_address,dut.frame_store.write_data}!=
                   {expected_address,expected_ink})
                    $fatal(1,"frame write address/color transaction changed");
                if(dut.frame_store.write_page==dut.frame_store.read_page ||
                   dut.frame_store.write_page==dut.frame_store.published_page)
                    $fatal(1,"frame write reaches a reader-owned page");
                checked_writes<=checked_writes+1;
            end
            expected_valid<=dot_tick&&pending_valid[2];
            if(dot_tick)begin
                pending_valid<={pending_valid[1:0],raster>=16&&raster<=287&&native_cycle>=13&&native_cycle<=60};
                pending_background0<={pending_background0[1:0],dut.ink_background0};
                pending_border<={pending_border[1:0],dut.main_border||dut.vertical_border};
                pending_address[0]<=17'((int'(raster)-16)*384+(int'(native_cycle)-13)*8+int'(dot));
                pending_ink[0]<=dut.ink;
                for(integer stage=1;stage<3;stage=stage+1)begin
                    pending_address[stage]<=pending_address[stage-1];
                    pending_ink[stage]<=pending_ink[stage-1];
                end
                expected_address<=pending_address[2];
                expected_ink<=pending_border[2]?dut.regs[6'h20][3:0]:
                    pending_background0[2]?dut.regs[6'h21][3:0]:pending_ink[2];
            end
            if(frame_done)begin
                if(write_pending||(|pending_valid))$fatal(1,"frame publication overlaps pending write");
                publications<=publications+1;
            end
            if(dot_tick)checked_pixels<=checked_pixels+1;
            if(|dut.ss_event || |dut.bg_event)
                collision_sys_samples<=collision_sys_samples+1;
        end
    end
    always @(posedge pixel_clk)
        if(!reset && dut.frame_store.have_frame && de)
            displayed_pixels<=displayed_pixels+1;
endmodule
