// 8 KiB direct-mapped instruction cache. The tag and word reads are synchronous,
// and a replacement only becomes visible once all eight words have arrived.
module fes_sh4_icache (
    input  logic        clk,
    input  logic        reset,
    input  logic        flush,
    input  logic        invalidate,
    input  logic [7:0]  invalidate_index,
    input  logic        lookup,
    input  logic [31:0] lookup_pc,
    input  logic        cacheable,
    output logic        instruction_valid,
    output logic [15:0] instruction,
    input  logic        consume,
    output logic        read_valid,
    output logic [31:0] read_addr,
    input  logic        read_ready,
    input  logic        response_valid,
    input  logic [31:0] response_data
);
    typedef enum logic [2:0] {
        LOOKUP, CHECK, REQUEST, REPLY, PRESENT, DISCARD
    } state_t;

    state_t state;
    (* ramstyle = "M20K" *) logic [31:0] words [0:2047];
    (* ramstyle = "M20K" *) logic [18:0] tags [0:255];
    logic [255:0] valid_lines;
    logic [31:1] pc;
    logic [31:0] word_q, requested_word;
    logic [18:0] tag_q;
    logic line_valid_q, use_cache;
    logic [2:0] fill_word;
    logic start_lookup, start_ram_read, fill, last_fill;

    assign start_lookup = state == LOOKUP && lookup && !lookup_pc[0] && !flush;
    assign start_ram_read = start_lookup && cacheable;
    assign fill = state == REPLY && response_valid && use_cache && !flush;
    assign last_fill = fill && fill_word == 3'd7;
    assign read_valid = state == REQUEST && !flush;
    assign read_addr = use_cache ? {pc[31:5], fill_word, 2'b00} : {pc[31:2], 2'b00};
    assign instruction_valid = state == PRESENT;
    assign instruction = pc[1] ? requested_word[31:16] : requested_word[15:0];

    always_ff @(posedge clk) begin
        if (start_ram_read) begin
            word_q <= words[lookup_pc[12:2]];
            tag_q <= tags[lookup_pc[12:5]];
            line_valid_q <= valid_lines[lookup_pc[12:5]];
        end
        if (fill)
            words[{pc[12:5], fill_word}] <= response_data;
        if (last_fill)
            tags[pc[12:5]] <= pc[31:13];
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            state <= LOOKUP;
            valid_lines <= '0;
        end else begin
            if (invalidate)
                valid_lines[invalidate_index] <= 1'b0;

            if (flush) begin
                // Once the arbiter has accepted a read it must complete it.
                // Drain its response without changing the cache or issuing it.
                if ((state == REPLY || state == DISCARD) && !response_valid)
                    state <= DISCARD;
                else
                    state <= LOOKUP;
            end else begin
                case (state)
                    LOOKUP: if (start_lookup) begin
                        pc <= lookup_pc[31:1];
                        use_cache <= cacheable;
                        fill_word <= 3'd0;
                        state <= cacheable ? CHECK : REQUEST;
                    end
                    CHECK: begin
                        if (line_valid_q && tag_q == pc[31:13]) begin
                            requested_word <= word_q;
                            state <= PRESENT;
                        end else begin
                            valid_lines[pc[12:5]] <= 1'b0;
                            state <= REQUEST;
                        end
                    end
                    REQUEST: if (read_ready)
                        state <= REPLY;
                    REPLY: if (response_valid) begin
                        if (!use_cache || fill_word == pc[4:2])
                            requested_word <= response_data;
                        if (!use_cache || last_fill) begin
                            if (last_fill)
                                valid_lines[pc[12:5]] <= 1'b1;
                            state <= PRESENT;
                        end else begin
                            fill_word <= fill_word + 3'd1;
                            state <= REQUEST;
                        end
                    end
                    PRESENT: if (consume)
                        state <= LOOKUP;
                    DISCARD: if (response_valid)
                        state <= LOOKUP;
                    default: state <= LOOKUP;
                endcase
            end
        end
    end
endmodule
