// SPDX-License-Identifier: MIT
// Three-buffer native-color handoff. Mailbox data stays stable until ack.
module c64_frame_store (
    input wire clk_sys, pixel_clk, reset,
    input wire write_enable, frame_done,
    input wire [16:0] write_address,
    input wire [3:0] write_data,
    input wire frame_start,
    input wire [16:0] read_address,
    output wire [3:0] read_data,
    output reg [1:0] write_page, read_page,
    output wire pending
);
`ifdef FES_SIMULATION
    reg [3:0] page0 [0:104447];
    reg [3:0] page1 [0:104447];
    reg [3:0] page2 [0:104447];
    reg [3:0] q0, q1, q2;
    always @(posedge clk_sys) begin
        if (!reset && write_enable) begin
            case (write_page)
                0: page0[write_address] <= write_data;
                1: page1[write_address] <= write_data;
                2: page2[write_address] <= write_data;
            endcase
        end
    end
    always @(posedge pixel_clk) begin
        q0 <= page0[read_address]; q1 <= page1[read_address]; q2 <= page2[read_address];
    end
`else
    wire [3:0] page_data [0:2];
    reg [5:0] read_bank_q;
    always @(posedge pixel_clk) read_bank_q <= read_address[16:11];
    wire [3:0] q0 = page_data[0];
    wire [3:0] q1 = page_data[1];
    wire [3:0] q2 = page_data[2];
    for (genvar page=0; page<3; page=page+1) begin: pages
        wire [4:0] bank_data [0:50];
        assign page_data[page]=bank_data[read_bank_q][3:0];
        for (genvar bank=0; bank<51; bank=bank+1) begin: banks
            // Dense 2048x5 uses one M10K for 2048 four-bit pixels.
            // The inferred ACLR memory pass otherwise chooses 1024x10.
            wire enabled = !reset && write_enable && write_page==2'(page) &&
                write_address[16:11]==6'(bank);
            MISTRAL_M10K #(.CFG_ABITS(11),.CFG_DBITS(5),.CFG_DUAL_CLOCK(1),
                .CFG_ASYNC_READ(0),.INIT(10240'b0)) ram (
                .CLK1(clk_sys),.CLK2(pixel_clk),.A1ADDR(write_address[10:0]),
                .A1DATA({1'b0,write_data}),.A1EN(!enabled),
                .B1ADDR(read_address[10:0]),.B1EN(1'b1),.B1DATA(bank_data[bank]),
                .ACLR0(1'b0),.ACLR1(1'b0));
        end
    end
`endif
    reg [1:0] published_page, read_page_q;
    reg publish_toggle, acknowledge, have_frame, have_frame_q;
    (* async_reg="true" *) reg ack_meta, ack_sync;
    (* async_reg="true" *) reg request_meta, request_sync;
    assign pending = publish_toggle != ack_sync;
    assign read_data = !have_frame_q ? 4'd0 :
        read_page_q==0 ? q0 : read_page_q==1 ? q1 : q2;
    integer candidate;
    always @(posedge clk_sys) begin
        ack_meta <= acknowledge; ack_sync <= ack_meta;
        if (reset) begin
            write_page <= 0; published_page <= 2; publish_toggle <= 0;
            ack_meta <= 0; ack_sync <= 0;
        end else begin
            if (frame_done && !pending) begin
                // With no pending publication, published_page is reader-owned.
                // The remaining page can be written while this frame is queued.
                published_page <= write_page; publish_toggle <= ~publish_toggle;
                for (candidate=0; candidate<3; candidate=candidate+1)
                    if (candidate != write_page && candidate != published_page)
                        write_page <= 2'(candidate);
            end
            // If HDMI has not acknowledged, overwrite only the private page.
            // Both the reader and the queued complete page stay untouched.
        end
    end
    always @(posedge pixel_clk) begin
        request_meta <= publish_toggle; request_sync <= request_meta;
        read_page_q <= read_page; have_frame_q <= have_frame;
        if (reset) begin
            read_page <= 2; acknowledge <= 0; have_frame <= 0;
            request_meta <= 0; request_sync <= 0; have_frame_q <= 0;
        end else if (frame_start && request_sync != acknowledge) begin
            read_page <= published_page; acknowledge <= request_sync; have_frame <= 1;
        end
    end
endmodule
