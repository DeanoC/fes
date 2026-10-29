// SPDX-License-Identifier: GPL-3.0-or-later
#include "probe.hpp"

namespace {
// QEMU virt's PL011. This address must never be used for DE25 hardware.
constexpr std::uintptr_t uart_base = 0x09000000u;
std::uint64_t counter_frequency;
std::uint64_t counter_start;

void initialize_uart()
{
    auto* uart = reinterpret_cast<volatile std::uint32_t*>(uart_base);
    uart[0x30 / 4] = 0;
    uart[0x44 / 4] = 0x7ffu;
    // QEMU virt's PL011 has a 24 MHz reference clock: 115200 baud, 8N1.
    uart[0x24 / 4] = 13;
    uart[0x28 / 4] = 1;
    uart[0x2c / 4] = 3u << 5;
    uart[0x30 / 4] = (1u << 9) | (1u << 8) | 1u;
    asm volatile("dsb sy" ::: "memory");
}

void put(char value)
{
    auto* uart = reinterpret_cast<volatile std::uint32_t*>(uart_base);
    while ((uart[6] & (1u << 5)) != 0) {}
    uart[0] = static_cast<unsigned char>(value);
    asm volatile("dsb sy" ::: "memory");
}

void print(const char* value)
{
    while (*value) {
        if (*value == '\n') put('\r');
        put(*value++);
    }
}

void number(std::uint64_t value)
{
    char digits[20];
    unsigned count = 0;
    do {
        digits[count++] = '0' + value % 10;
        value /= 10;
    } while (value);
    while (count) put(digits[--count]);
}

std::uint64_t counter()
{
    std::uint64_t value;
    asm volatile("isb; mrs %0, cntpct_el0" : "=r"(value));
    return value;
}

std::uint64_t clock_us()
{
    const std::uint64_t elapsed = counter() - counter_start;
    return elapsed / counter_frequency * 1000000u +
        elapsed % counter_frequency * 1000000u / counter_frequency;
}

void idle()
{
    // Nonblocking hint: Link owns the deadline and finite stall/poll bounds.
    asm volatile("yield");
}

void report(const char* name, bool passed)
{
    print(passed ? "PASS " : "FAIL ");
    print(name);
    print("\n");
}

[[noreturn]] void stop()
{
    for (;;) asm volatile("wfe");
}
}

extern "C" [[noreturn]] void baremetal_main()
{
    initialize_uart();
    std::uint64_t current_el;
    asm volatile("mrs %0, CurrentEL" : "=r"(current_el));
    current_el >>= 2;
    print("FES bare-metal AArch64 probe; QEMU virt; EL=");
    number(current_el);
    print("; transport=emulated; no DE25 MMIO\n");
    if (current_el < 1 || current_el > 3) {
        print("FES_BAREMETAL_PROBE FAIL invalid exception level\n");
        stop();
    }
    // The selected QEMU entry permits physical-counter access. Real firmware
    // may trap it; a Nano port must establish that privilege contract first.
    asm volatile("mrs %0, cntfrq_el0" : "=r"(counter_frequency));
    if (counter_frequency == 0) {
        print("FES_BAREMETAL_PROBE FAIL missing counter frequency\n");
        stop();
    }
    counter_start = counter();
    bool passed = false;
    const unsigned count = fes_probe::selftest(report, &passed, clock_us, idle);
    print(passed ? "FES_BAREMETAL_PROBE PASS tests=" : "FES_BAREMETAL_PROBE FAIL tests=");
    number(count);
    print(" transport=emulated os=none\n");
    stop();
}
