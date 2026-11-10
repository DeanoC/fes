package census_pkg;
    typedef struct packed {
        logic [7:0] data;
        logic [3:0] tag;
    } rec_t;
    typedef enum logic [1:0] { S_IDLE, S_RUN, S_DONE } state_t;
    localparam int OUT_W = 12;
    localparam int SAMPLE_W = 16;
    // opl2_pkg.sv:79 form: signed'() in a localparam expression
    localparam DAC_SHIFT = signed'(OUT_W - SAMPLE_W - 2) < 0 ? 0 : OUT_W - SAMPLE_W - 3;
endpackage
