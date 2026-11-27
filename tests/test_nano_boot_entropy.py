"""Execute the production boot-entropy backend against SDM reply and custody faults."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / "platform/embedded/firmware"


class BootEntropy(unittest.TestCase):
    def test_sdm_replies_and_runtime_isolation(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            headers = {
                "common/debug.h": "#define LOG_LEVEL 20\n#define LOG_LEVEL_VERBOSE 50\nvoid test_panic(void);\n#define panic() test_panic()\n",
                "common/runtime_svc.h": '''#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>
typedef struct { uint8_t time_low[4], rest[12]; } uuid_t;
#define DEFINE_SVC_UUID2(name, ...) static const uuid_t name = {{1}, {0}}
''',
                "lib/mmio.h": "uint32_t mmio_read_32(uintptr_t);\n",
                "lib/utils.h": "#define zeromem(p,n) memset((p),0,(n))\n",
                "plat/common/plat_trng.h": "",
                "socfpga_fcs.h": "#define FCS_RANDOM_WORD_SIZE 8U\n",
                "socfpga_mailbox.h": '''#define MBOX_OFFSET 0
#define MBOX_CIN 0
#define MBOX_ROUT 4
#define MBOX_MAX_IND_JOB_ID 14
#define MBOX_JOB_ID 15
#define MBOX_FCS_RANDOM_GEN 0x80
#define CMD_CASUAL 0
#define MBOX_CMD_BUFFER_SIZE 32
#define MBOX_RESP_BUFFER_SIZE 16
#define MBOX_RET_OK 0
int mailbox_send_cmd(uint32_t,uint32_t,uint32_t *,unsigned,uint32_t,uint32_t *,unsigned *);
''',
            }
            for name, contents in headers.items():
                path = tmp / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(contents)
            output = tmp / "boot-entropy"
            subprocess.run(["cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-I", str(tmp),
                            str(FW / "tests/nano_trng_test.c"), "-o", str(output)], check=True)
            subprocess.run([output], check=True)
