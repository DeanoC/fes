// 16 KiB direct-mapped operand cache with serialized refills and write-backs.
module fes_sh4_dcache (
    input  logic        clk,
    input  logic        reset,
    input  logic        invalidate_all,
    input  logic        request,
    output logic        ready,
    input  logic [31:2] address,
    input  logic [13:2] index,
    input  logic        array_access,
    input  logic        array_data,
    input  logic        array_assoc,
    input  logic        address_cacheable,
    input  logic        address_copyback,
    input  logic        purge_before_read,
    input  logic [1:0]  maintenance,
    input  logic        allocate_without_read,
    input  logic [3:0]  wstrb,
    input  logic [31:0] wdata,
    output logic        complete,
    output logic [31:0] rdata,
    output logic        bus_request,
    output logic        bus_writeback,
    input  logic        bus_ready,
    output logic [31:0] bus_address,
    output logic [3:0]  bus_wstrb,
    output logic [31:0] bus_wdata,
    input  logic        bus_response,
    input  logic [31:0] bus_rdata
);
    typedef enum logic [3:0] {IDLE, CHECK, WB_READ, WB_SEND, REQUEST, REPLY, UPDATE, ALLOCATE, ARRAY_WRITE, DONE} state_t;

    state_t state;
    (* ramstyle = "M20K" *) logic [31:0] words [0:4095];
    (* ramstyle = "M20K" *) logic [21:0] tags [0:511];
    logic [511:0] valid_lines, dirty_lines;
    logic [31:2] saved_addr;
    logic [13:2] saved_index;
    logic saved_array, saved_data_array, saved_assoc, array_tag_write;
    logic [3:0] saved_wstrb;
    logic [31:0] saved_wdata, word_q;
    logic [21:0] tag_q;
    logic [2:0] line_word;
    logic lookup, line_valid_q, line_dirty_q, use_cache, copyback, purge, hit, alias_hit, store_hit;
    logic write_access, fills_line, discard_line, write_back;
    logic [1:0] line_maintenance;
    logic allocate, maintenance_hit, allocate_word, last_allocate;
    logic refill, last_refill, store_complete;
    logic [11:0] ram_read_addr, ram_write_addr;
    logic [31:0] ram_write_data;
    logic [3:0] ram_write_mask;

    assign ready = state == IDLE;
    assign complete = state == DONE;
    assign lookup = request && ready;
    assign hit = line_valid_q && tag_q == saved_addr[31:10];
    // Direct P1/P2 aliases and translated physical addresses can name the same
    // external memory. P4 device addresses retain their full tags.
    assign alias_hit = hit || (line_valid_q && tag_q[21:19] != 3'b111 &&
                               saved_addr[31:29] != 3'b111 && tag_q[18:0] == saved_addr[28:10]);
    assign write_access = saved_wstrb != 0;
    assign fills_line = use_cache && (!write_access || copyback);
    assign discard_line = !hit && fills_line ||
                          (!use_cache && (write_access || (alias_hit && purge))) ||
                          (use_cache && !hit && alias_hit);
    assign write_back = line_valid_q && line_dirty_q &&
                        ((!use_cache && alias_hit) || discard_line);

    assign maintenance_hit = use_cache && alias_hit;
    assign allocate_word = state == ALLOCATE;
    assign last_allocate = allocate_word && line_word == 3'd7;

    assign bus_writeback = state == WB_SEND;
    assign bus_request = state == REQUEST || bus_writeback;
    assign bus_address = bus_writeback ? {tag_q, saved_addr[9:5], line_word, 2'b00} :
                         fills_line ? {saved_addr[31:5], line_word, 2'b00} : {saved_addr, 2'b00};
    assign bus_wstrb = bus_writeback ? 4'b1111 : fills_line ? 4'b0 : saved_wstrb;
    assign bus_wdata = bus_writeback ? word_q : saved_wdata;

    assign refill = state == REPLY && bus_response && fills_line;
    assign last_refill = refill && line_word == 3'd7;
    assign store_complete = state == REQUEST && bus_ready && write_access && !fills_line;
    assign array_tag_write = state == ARRAY_WRITE && !saved_data_array && !saved_assoc;
    assign ram_read_addr = state == WB_READ ? {saved_index[13:5], line_word} : index;
    assign ram_write_addr = refill || allocate_word ? {saved_index[13:5], line_word} : saved_index;
    // Initialize undefined words on MOVCA allocation so a dirty line never
    // writes words left over from an unrelated physical address.
    assign ram_write_data = refill ? bus_rdata :
                            allocate_word && line_word != saved_addr[4:2] ? 32'b0 : saved_wdata;
    assign ram_write_mask = refill || allocate_word ? 4'b1111 :
                            (state == UPDATE || (store_complete && store_hit) ||
                             (state == ARRAY_WRITE && saved_data_array)) ? saved_wstrb : 4'b0000;

    always_ff @(posedge clk) begin
        if (lookup || state == WB_READ)
            word_q <= words[ram_read_addr];
        if (lookup) begin
            tag_q <= tags[index[13:5]];
            line_valid_q <= valid_lines[index[13:5]];
            line_dirty_q <= dirty_lines[index[13:5]];
        end

        for (int lane = 0; lane < 4; lane++) begin
            if (ram_write_mask[lane])
                words[ram_write_addr][8*lane +: 8] <= ram_write_data[8*lane +: 8];
        end
        if (last_refill || last_allocate || array_tag_write)
            tags[saved_index[13:5]] <= array_tag_write ? {3'b0, saved_wdata[28:10]} : saved_addr[31:10];
    end

    always_ff @(posedge clk) begin
        if (reset || invalidate_all) begin
            state <= IDLE;
            valid_lines <= '0;
            dirty_lines <= '0;
        end else begin
            case (state)
                IDLE: if (request) begin
                    saved_addr <= address;
                    saved_index <= index;
                    saved_array <= array_access;
                    saved_data_array <= array_data;
                    saved_assoc <= array_assoc;
                    saved_wstrb <= wstrb;
                    saved_wdata <= wdata;
                    use_cache <= address_cacheable;
                    copyback <= address_copyback;
                    purge <= purge_before_read;
                    line_maintenance <= maintenance;
                    allocate <= allocate_without_read && address_cacheable && address_copyback;
                    line_word <= 3'd0;
                    store_hit <= 1'b0;
                    state <= CHECK;
                end
                CHECK: begin
                    if (saved_array) begin
                        if (!write_access) begin
                            rdata <= saved_data_array ? word_q : {3'b0, tag_q[18:0], 8'b0,
                                                                 line_dirty_q, line_valid_q};
                            state <= DONE;
                        end else if (saved_data_array)
                            state <= ARRAY_WRITE;
                        else if (saved_assoc && (!line_valid_q || tag_q[18:0] != saved_wdata[28:10]))
                            state <= DONE;
                        else if (line_valid_q && line_dirty_q && (!saved_assoc || saved_wdata[1:0] != 2'b11))
                            state <= WB_READ;
                        else
                            state <= ARRAY_WRITE;
                    end else if (line_maintenance != 0) begin
                        if (maintenance_hit && line_maintenance[1] && line_dirty_q)
                            state <= WB_READ;
                        else begin
                            if (maintenance_hit && line_maintenance[0]) begin
                                valid_lines[saved_index[13:5]] <= 1'b0;
                                dirty_lines[saved_index[13:5]] <= 1'b0;
                            end
                            state <= DONE;
                        end
                    end else if (use_cache && hit && !write_access) begin
                        rdata <= word_q;
                        state <= DONE;
                    end else if (use_cache && hit && copyback) begin
                        state <= UPDATE;
                    end else if (write_back)
                        state <= WB_READ;
                    else begin
                        store_hit <= use_cache && hit;
                        if (discard_line) begin
                            valid_lines[saved_index[13:5]] <= 1'b0;
                            dirty_lines[saved_index[13:5]] <= 1'b0;
                        end
                        state <= allocate ? ALLOCATE : REQUEST;
                    end
                end
                WB_READ: state <= WB_SEND;
                WB_SEND: if (bus_ready) begin
                    line_word <= line_word + 3'd1;
                    if (line_word == 3'd7) begin
                        dirty_lines[saved_index[13:5]] <= 1'b0;
                        if (line_maintenance == 0 || line_maintenance[0])
                            valid_lines[saved_index[13:5]] <= 1'b0;
                        if (saved_array)
                            state <= ARRAY_WRITE;
                        else if (line_maintenance != 0)
                            state <= DONE;
                        else
                            state <= allocate ? ALLOCATE : REQUEST;
                    end else
                        state <= WB_READ;
                end
                REQUEST: if (bus_ready)
                    state <= bus_wstrb == 0 ? REPLY : DONE;
                REPLY: if (bus_response) begin
                    if (!fills_line || line_word == saved_addr[4:2])
                        rdata <= bus_rdata;
                    if (!fills_line || last_refill) begin
                        if (last_refill)
                            valid_lines[saved_index[13:5]] <= 1'b1;
                        state <= write_access ? UPDATE : DONE;
                    end else begin
                        line_word <= line_word + 3'd1;
                        state <= REQUEST;
                    end
                end
                UPDATE: begin
                    dirty_lines[saved_index[13:5]] <= 1'b1;
                    state <= DONE;
                end
                ALLOCATE: begin
                    line_word <= line_word + 3'd1;
                    if (last_allocate) begin
                        valid_lines[saved_index[13:5]] <= 1'b1;
                        dirty_lines[saved_index[13:5]] <= 1'b1;
                        state <= DONE;
                    end
                end
                ARRAY_WRITE: begin
                    if (!saved_data_array) begin
                        valid_lines[saved_index[13:5]] <= saved_wdata[0];
                        dirty_lines[saved_index[13:5]] <= saved_wdata[1];
                    end
                    state <= DONE;
                end
                DONE: state <= IDLE;
                default: state <= IDLE;
            endcase
        end
    end
endmodule
