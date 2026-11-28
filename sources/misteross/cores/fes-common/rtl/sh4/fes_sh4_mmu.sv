// Translates P0/P3 requests selected by the CPU. Direct regions and the P4
// control-register addresses are decoded by the CPU before this interface.
module fes_sh4_mmu (
    input  logic        clk,
    input  logic        reset,
    input  logic        request,
    output logic        ready,
    input  logic        instruction,
    input  logic        write,
    input  logic        supervisor,
    input  logic        address_exception,
    input  logic [31:0] fault_address,
    input  logic [31:0] virtual_addr,
    output logic        response,
    output logic [31:0] physical_addr,
    output logic        cacheable,
    output logic        write_through,
    output logic [3:0]  fault,
    output logic        enabled,
    input  logic [3:0]  reg_select,
    input  logic        reg_write,
    input  logic [31:0] reg_wdata,
    output logic [31:0] reg_rdata,
    input  logic        ldtlb
);
    typedef struct packed {
        logic [31:10] vpn;
        logic [28:10] ppn;
        logic [7:0] asid;
        logic [1:0] size;
        logic [1:0] prot;
        logic shared_page, cached, dirty, wt;
    } entry_t;
    typedef enum logic [2:0] {IDLE, MATCH, GROUP, SELECT, RESULT, RESPOND} state_t;
    localparam logic [3:0] MISS = 4'd5, MULTIPLE = 4'd6, PROTECTION = 4'd7, INITIAL_WRITE = 4'd8;

    state_t state;
    entry_t utlb [0:63];
    entry_t itlb [0:3];
    logic [63:0] utlb_valid, umatch;
    logic [3:0] itlb_valid, imatch;
    logic [21:0] pteh_vpn;
    logic [7:0] asid;
    logic [28:0] ptel;
    logic [3:0] ptea;
    logic [31:0] ttb, tea;
    logic [5:0] urc, urb, lrui;
    logic sv, sqmd;

    logic [31:0] va;
    logic fetch, store, privileged;
    logic [7:0] group_hit, group_multi;
    entry_t group_entry [0:7];
    entry_t ientry, uentry, selected;
    logic [1:0] iway, victim, selected_way;
    logic ihit, found, multiple, use_utlb;
    logic [3:0] result_fault;

    function automatic logic page_matches(input logic [31:10] vpn, addr,
                                          input logic [1:0] page_size,
                                          input logic [7:0] entry_asid, current_asid,
                                          input logic ignore_asid);
        return addr[31:20] == vpn[31:20] &&
               (page_size == 3 || addr[19:16] == vpn[19:16]) &&
               (page_size >= 2 || addr[15:12] == vpn[15:12]) &&
               (page_size != 0 || addr[11:10] == vpn[11:10]) &&
               (ignore_asid || current_asid == entry_asid);
    endfunction

    function automatic logic [5:0] touch(input logic [5:0] old, input logic [1:0] way);
        case (way)
            0: return old & 6'b000111;
            1: return (old & 6'b011001) | 6'b100000;
            2: return (old & 6'b101010) | 6'b010100;
            3: return (old & 6'b110100) | 6'b001011;
        endcase
    endfunction

    always_comb begin
        casez (lrui)
            6'b111???: victim = 0;
            6'b0??11?: victim = 1;
            6'b?0?0?1: victim = 2;
            default: victim = 3;
        endcase
        ientry = '0;
        iway = '0;
        for (int i = 0; i < 4; i++) begin
            if (imatch[i]) begin
                ientry |= itlb[i];
                iway |= 2'(i);
            end
        end
        uentry = '0;
        for (int i = 0; i < 8; i++)
            uentry |= group_entry[i];
    end

    always_comb begin
        case (reg_select)
            0: reg_rdata = {pteh_vpn, 2'b00, asid};
            1: reg_rdata = {3'b000, ptel};
            2: reg_rdata = ttb;
            3: reg_rdata = tea;
            4: reg_rdata = {lrui, 2'b0, urb, 2'b0, urc, sqmd, sv, 7'b0, enabled};
            13: reg_rdata = {28'b0, ptea};
            default: reg_rdata = 32'b0;
        endcase
    end

    assign ready = state == IDLE;
    assign response = state == RESPOND;
    assign ihit = fetch && |imatch;
    assign result_fault = multiple ? MULTIPLE : !found ? MISS :
                          (!privileged && !selected.prot[1]) ||
                          (store && !selected.prot[0]) ? PROTECTION :
                          store && !selected.dirty ? INITIAL_WRITE : 4'b0;

    // Register the match vector, then reduce it in eight parallel banks.
    // The selected entry and its permissions are checked in a separate stage.
    for (genvar bank = 0; bank < 8; bank++) begin : banks
        logic [7:0] mask;
        entry_t reduced;
        assign mask = umatch[bank * 8 +: 8];
        always_comb begin
            reduced = '0;
            for (int i = 0; i < 8; i++)
                if (mask[i])
                    reduced |= utlb[bank * 8 + i];
        end
        always_ff @(posedge clk) if (state == GROUP) begin
            group_entry[bank] <= reduced;
            group_hit[bank] <= |mask;
            group_multi[bank] <= |(mask & (mask - 8'd1));
        end
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            state <= IDLE;
            utlb_valid <= '0;
            itlb_valid <= '0;
            pteh_vpn <= '0;
            asid <= '0;
            ptel <= '0;
            ptea <= '0;
            ttb <= '0;
            tea <= '0;
            urc <= '0;
            urb <= '0;
            lrui <= '0;
            sv <= 1'b0;
            sqmd <= 1'b0;
            enabled <= 1'b0;
        end else begin
            if (address_exception)
                tea <= fault_address;
            case (state)
                IDLE: begin
                    if (reg_write) begin
                        case (reg_select)
                            0: {pteh_vpn, asid} <= {reg_wdata[31:10], reg_wdata[7:0]};
                            1: ptel <= {reg_wdata[28:10], 1'b0, reg_wdata[8:0]};
                            2: ttb <= reg_wdata;
                            3: tea <= reg_wdata;
                            4: begin
                                {lrui, urb, urc} <= {reg_wdata[31:26], reg_wdata[23:18], reg_wdata[15:10]};
                                {sqmd, sv, enabled} <= {reg_wdata[9:8], reg_wdata[0]};
                                if (reg_wdata[2]) begin
                                    utlb_valid <= '0;
                                    itlb_valid <= '0;
                                end
                            end
                            13: ptea <= reg_wdata[3:0];
                            default: ;
                        endcase
                    end else if (ldtlb) begin
                        utlb[urc] <= {pteh_vpn, ptel[28:10], asid, ptel[7], ptel[4],
                                       ptel[6:5], ptel[1], ptel[3], ptel[2], ptel[0]};
                        utlb_valid[urc] <= ptel[8];
                    end else if (request) begin
                        va <= virtual_addr;
                        fetch <= instruction;
                        store <= write;
                        privileged <= supervisor;
                        state <= MATCH;
                    end
                end
                MATCH: begin
                    for (int i = 0; i < 64; i++)
                        umatch[i] <= utlb_valid[i] && page_matches(utlb[i].vpn, va[31:10], utlb[i].size,
                                                 utlb[i].asid, asid, utlb[i].shared_page || (sv && privileged));
                    for (int i = 0; i < 4; i++)
                        imatch[i] <= itlb_valid[i] && page_matches(itlb[i].vpn, va[31:10], itlb[i].size,
                                                 itlb[i].asid, asid, itlb[i].shared_page || (sv && privileged));
                    state <= GROUP;
                end
                GROUP: state <= SELECT;
                SELECT: begin
                    use_utlb <= !ihit;
                    selected <= ihit ? ientry : uentry;
                    selected_way <= ihit ? iway : victim;
                    found <= ihit || |group_hit;
                    multiple <= ihit ? |(imatch & (imatch - 4'd1)) :
                                |group_multi || |(group_hit & (group_hit - 8'd1));
                    state <= RESULT;
                end
                RESULT: begin
                    physical_addr <= {3'b0, selected.ppn[28:20],
                                      selected.size < 3 ? selected.ppn[19:16] : va[19:16],
                                      selected.size < 2 ? selected.ppn[15:12] : va[15:12],
                                      selected.size == 0 ? selected.ppn[11:10] : va[11:10], va[9:0]};
                    cacheable <= selected.cached;
                    write_through <= selected.wt;
                    fault <= result_fault;
                    if (use_utlb)
                        urc <= urb != 0 && urc + 6'd1 == urb ? 6'd0 : urc + 6'd1;
                    if (result_fault != 0) begin
                        tea <= va;
                        pteh_vpn <= va[31:10];
                    end
                    if (fetch && found && !multiple) begin
                        lrui <= touch(lrui, selected_way);
                        if (use_utlb) begin
                            itlb[selected_way] <= selected;
                            itlb_valid[selected_way] <= 1'b1;
                        end
                    end
                    state <= RESPOND;
                end
                RESPOND: state <= IDLE;
                default: state <= IDLE;
            endcase
        end
    end
endmodule
