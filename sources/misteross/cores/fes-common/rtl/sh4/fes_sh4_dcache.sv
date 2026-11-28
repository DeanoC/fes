// 16 KiB direct-mapped operand cache. Loads allocate 32-byte lines. Writes
// are write-through, updating resident bytes on hits without allocating misses.
module fes_sh4_dcache (
    input  logic        clk,
    input  logic        reset,
    input  logic        request,
    output logic        ready,
    input  logic [31:2] address,
    input  logic        address_cacheable,
    input  logic [3:0]  wstrb,
    input  logic [31:0] wdata,
    output logic        complete,
    output logic [31:0] rdata,
    output logic        bus_request,
    input  logic        bus_ready,
    output logic [31:0] bus_address,
    output logic [3:0]  bus_wstrb,
    output logic [31:0] bus_wdata,
    input  logic        bus_response,
    input  logic [31:0] bus_rdata
);
    typedef enum logic [2:0] {IDLE, CHECK, REQUEST, REPLY, DONE} state_t;

    state_t state;
    (* ramstyle = "M20K" *) logic [31:0] words [0:4095];
    (* ramstyle = "M20K" *) logic [17:0] tags [0:511];
    logic [511:0] valid_lines;
    logic [31:2] saved_addr;
    logic [3:0] saved_wstrb;
    logic [31:0] saved_wdata, word_q;
    logic [17:0] tag_q;
    logic [2:0] fill_word;
    logic lookup, line_valid_q, use_cache, hit, store_hit;
    logic refill, last_refill, store_complete;
    logic [11:0] ram_write_addr;
    logic [31:0] ram_write_data;
    logic [3:0] ram_write_mask;

    assign ready = state == IDLE;
    assign complete = state == DONE;
    assign lookup = request && ready && address_cacheable;
    assign hit = line_valid_q && tag_q == saved_addr[31:14];

    assign bus_request = state == REQUEST;
    assign bus_address = use_cache && saved_wstrb == 0 ?
                         {saved_addr[31:5], fill_word, 2'b00} : {saved_addr, 2'b00};
    assign bus_wstrb = saved_wstrb;
    assign bus_wdata = saved_wdata;

    assign refill = state == REPLY && bus_response && use_cache;
    assign last_refill = refill && fill_word == 3'd7;
    assign store_complete = bus_request && bus_ready && saved_wstrb != 0;
    assign ram_write_addr = refill ? {saved_addr[13:5], fill_word} : saved_addr[13:2];
    assign ram_write_data = refill ? bus_rdata : saved_wdata;
    assign ram_write_mask = refill ? 4'b1111 :
                            store_complete && store_hit ? saved_wstrb : 4'b0000;

    always_ff @(posedge clk) begin
        if (lookup) begin
            word_q <= words[address[13:2]];
            tag_q <= tags[address[13:5]];
            line_valid_q <= valid_lines[address[13:5]];
        end
        for (int lane = 0; lane < 4; lane++) begin
            if (ram_write_mask[lane])
                words[ram_write_addr][8*lane +: 8] <= ram_write_data[8*lane +: 8];
        end
        if (last_refill)
            tags[saved_addr[13:5]] <= saved_addr[31:14];
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            state <= IDLE;
            valid_lines <= '0;
        end else begin
            case (state)
                IDLE: if (request) begin
                    saved_addr <= address;
                    saved_wstrb <= wstrb;
                    saved_wdata <= wdata;
                    use_cache <= address_cacheable;
                    fill_word <= 3'd0;
                    store_hit <= 1'b0;
                    state <= address_cacheable ? CHECK : REQUEST;
                end
                CHECK: begin
                    if (saved_wstrb == 0 && hit) begin
                        rdata <= word_q;
                        state <= DONE;
                    end else begin
                        store_hit <= hit && saved_wstrb != 0;
                        if (saved_wstrb == 0)
                            valid_lines[saved_addr[13:5]] <= 1'b0;
                        state <= REQUEST;
                    end
                end
                REQUEST: if (bus_ready) begin
                    if (saved_wstrb == 0) begin
                        state <= REPLY;
                    end else begin
                        // A store through another address alias must not
                        // leave an old copy at this index.
                        if (!store_hit)
                            valid_lines[saved_addr[13:5]] <= 1'b0;
                        state <= DONE;
                    end
                end
                REPLY: if (bus_response) begin
                    if (!use_cache || fill_word == saved_addr[4:2])
                        rdata <= bus_rdata;
                    if (!use_cache || last_refill) begin
                        if (last_refill)
                            valid_lines[saved_addr[13:5]] <= 1'b1;
                        state <= DONE;
                    end else begin
                        fill_word <= fill_word + 3'd1;
                        state <= REQUEST;
                    end
                end
                DONE: state <= IDLE;
                default: state <= IDLE;
            endcase
        end
    end
endmodule
