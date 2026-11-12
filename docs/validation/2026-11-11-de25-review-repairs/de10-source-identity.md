# DE10 source-identity audit after #1312 repairs

Candidate `cdb453111dc23389b5e028f592ae7c61e93059e8` compared with current main `bb5f1bfc14026b55113202fbc88b730241da9ee1`. Source-map proof only; full numeric functional BUILD_IDs were not computed. The earlier frozen9ea audit is retained separately, and the later main TV80 deletions are inherited from main.

| Profile | Main / candidate files | Source map |
| --- | --- | --- |
| build_fes_apple2_oss | 51 / 51 | Identical |
| build_fes_atari_st_oss | 71 / 71 | Identical |
| build_fes_c64_oss | 103 / 103 | Identical |
| build_fes_coleco_socket_v2 | 81 / 81 | Identical |
| build_fes_menu_package | 45 / 45 | Identical |
| build_fes_pc386_quartus | 115 / 115 | Identical |
| build_fes_pcxt_quartus | 176 / 176 | Identical |
| build_fes_pong | 28 / 28 | Identical |
| build_fes_ramtest | 44 / 44 | Identical |
| build_fes_riscv | 418 / 611 | RISC-V broad boards metadata differs; explicit pins identical |
| build_fes_sg1000_oss | 73 / 73 | Identical |
| build_fes_sms_oss | 76 / 76 | Identical |
| build_fes_spectrum_oss | 64 / 64 | Identical |
| build_fes_zx81 | 38 / 38 | Identical |
| build_fes_zx81_oss | 70 / 70 | Identical |

All20 DE10 identity-version2 recipes map to these profiles. Every tracked shared core/producer file matches current main, apart from the test-only Coleco audio bench outside those roots. DE25 changes live under boards/de25nano. The existing RISC-V fallback hashes that whole board root; its source identity changes although all28 explicit RTL/pinned inputs match. No all-DE10 BUILD_ID equality, netlist, RBF, timing/seal or HIL claim follows.

Detailed file maps and their canonical SHA256 values are retained in de10-closure-audit.json, closure-main.json and closure-current.json.
