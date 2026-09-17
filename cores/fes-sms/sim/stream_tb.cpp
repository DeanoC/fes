// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_computer_gp.h"
#include "Vfes_computer_gp_fes_computer_gp.h"
#include "verilated.h"

#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <map>
#include <sstream>
#include <string>
#include <vector>

namespace {

[[noreturn]] void fail(const std::string &message) {
    std::cerr << "FES SMS stream: " << message << '\n';
    std::exit(EXIT_FAILURE);
}

void require(bool condition, const std::string &message) {
    if (!condition) fail(message);
}

struct Json {
    enum Type { NUL, BOOL, NUM, STR, ARR, OBJ };
    Type type = NUL;
    bool b = false;
    uint32_t n = 0;
    std::string s;
    std::vector<Json> a;
    std::map<std::string, Json> o;

    bool has(const std::string &key) const {
        return type == OBJ && o.find(key) != o.end();
    }
    const Json &get(const std::string &key) const {
        require(has(key), "missing JSON key " + key);
        return o.at(key);
    }
    bool boolean(const std::string &key, bool fallback) const {
        if (!has(key)) return fallback;
        require(o.at(key).type == BOOL, key + " is not bool");
        return o.at(key).b;
    }
    uint32_t number(const std::string &key) const {
        require(get(key).type == NUM, key + " is not number");
        return get(key).n;
    }
    std::string str(const std::string &key) const {
        require(get(key).type == STR, key + " is not string");
        return get(key).s;
    }
};

class Parser {
public:
    explicit Parser(std::string text) : text_(std::move(text)) {}

    Json parse() {
        skip();
        Json value = parse_value();
        skip();
        require(pos_ == text_.size(), "trailing JSON");
        return value;
    }

private:
    std::string text_;
    size_t pos_ = 0;

    void skip() {
        while (pos_ < text_.size() &&
               (text_[pos_] == ' ' || text_[pos_] == '\n' || text_[pos_] == '\r' ||
                text_[pos_] == '\t'))
            ++pos_;
    }

    char peek() {
        skip();
        require(pos_ < text_.size(), "unexpected end of JSON");
        return text_[pos_];
    }

    char take() {
        char c = peek();
        ++pos_;
        return c;
    }

    bool starts(const char *lit) {
        skip();
        const size_t n = std::char_traits<char>::length(lit);
        return text_.compare(pos_, n, lit) == 0;
    }

    void eat(const char *lit) {
        require(starts(lit), std::string("expected ") + lit);
        pos_ += std::char_traits<char>::length(lit);
    }

    Json parse_value() {
        if (starts("null")) {
            eat("null");
            return Json();
        }
        if (starts("true")) {
            eat("true");
            Json j;
            j.type = Json::BOOL;
            j.b = true;
            return j;
        }
        if (starts("false")) {
            eat("false");
            Json j;
            j.type = Json::BOOL;
            j.b = false;
            return j;
        }
        const char c = peek();
        if (c == '"') return parse_string();
        if (c == '[') return parse_array();
        if (c == '{') return parse_object();
        if (c == '-' || (c >= '0' && c <= '9')) return parse_number();
        fail(std::string("unexpected JSON byte ") + c);
        return Json();
    }

    Json parse_number() {
        skip();
        const size_t start = pos_;
        if (text_[pos_] == '-') ++pos_;
        require(pos_ < text_.size() && text_[pos_] >= '0' && text_[pos_] <= '9',
                "bad JSON number");
        while (pos_ < text_.size() && text_[pos_] >= '0' && text_[pos_] <= '9')
            ++pos_;
        Json j;
        j.type = Json::NUM;
        j.n = uint32_t(std::stoul(text_.substr(start, pos_ - start), nullptr, 10));
        return j;
    }

    Json parse_string() {
        require(take() == '"', "string");
        std::string out;
        while (pos_ < text_.size() && text_[pos_] != '"') {
            if (text_[pos_] == '\\') fail("escaped JSON string");
            out.push_back(text_[pos_++]);
        }
        require(take() == '"', "unterminated string");
        Json j;
        j.type = Json::STR;
        j.s = out;
        return j;
    }

    Json parse_array() {
        require(take() == '[', "array");
        Json j;
        j.type = Json::ARR;
        skip();
        if (peek() == ']') {
            take();
            return j;
        }
        while (true) {
            j.a.push_back(parse_value());
            skip();
            if (peek() == ']') {
                take();
                return j;
            }
            require(take() == ',', "array comma");
        }
    }

    Json parse_object() {
        require(take() == '{', "object");
        Json j;
        j.type = Json::OBJ;
        skip();
        if (peek() == '}') {
            take();
            return j;
        }
        while (true) {
            Json key = parse_string();
            skip();
            require(take() == ':', "object colon");
            j.o[key.s] = parse_value();
            skip();
            if (peek() == '}') {
                take();
                return j;
            }
            require(take() == ',', "object comma");
        }
    }
};

Json read_json(const char *path) {
    std::ifstream stream(path);
    require(bool(stream), std::string("cannot open ") + path);
    std::ostringstream contents;
    contents << stream.rdbuf();
    return Parser(contents.str()).parse();
}

uint32_t crc32_ieee(const uint8_t *data, size_t length) {
    uint32_t crc = 0xffffffffu;
    for (size_t i = 0; i < length; ++i) {
        crc ^= data[i];
        for (int bit = 0; bit < 8; ++bit)
            crc = (crc & 1u) ? (crc >> 1) ^ 0xedb88320u : crc >> 1;
    }
    return crc ^ 0xffffffffu;
}

struct Mailbox {
    Vfes_computer_gp dut;

    Mailbox() {
        dut.clk = 0;
        dut.gpo = 0;
        dut.media_addr = 0;
        dut.eval();
    }

    Vfes_computer_gp_fes_computer_gp &core() { return *dut.fes_computer_gp; }

    void tick() {
        dut.clk = 1;
        dut.eval();
        dut.clk = 0;
        dut.eval();
    }

    uint8_t read_media(uint16_t address) {
        dut.media_addr = address;
        dut.eval();
        tick();
        return uint8_t(dut.media_q);
    }

    void wait_for_ack(bool toggle, uint32_t expected, const std::string &name) {
        const uint32_t previous = dut.gpi;
        for (unsigned cycle = 0; cycle != 16; ++cycle) {
            if (((uint32_t(dut.gpi) >> 23) & 1u) == unsigned(toggle)) {
                if (uint32_t(dut.gpi) != expected) {
                    std::ostringstream message;
                    message << name << ": gpi 0x" << std::hex << dut.gpi
                            << " expected 0x" << expected;
                    fail(message.str());
                }
                return;
            }
            require(uint32_t(dut.gpi) == previous, name + ": GPI changed before ACK");
            tick();
        }
        fail(name + ": ACK timeout");
    }
};

uint32_t command(bool toggle, uint8_t opcode, uint8_t index, uint16_t argument) {
    return (toggle ? 0x80000000u : 0u) | (uint32_t(opcode) << 24) |
           (uint32_t(index) << 16) | argument;
}

uint32_t response(bool toggle, bool error, uint16_t data) {
    return 0xf5000000u | (toggle ? 0x00800000u : 0u) |
           (error ? 0x00400000u : 0u) | data;
}

void exchange(Mailbox &mailbox, bool &toggle, uint8_t opcode, uint8_t index,
              uint16_t argument, uint32_t expected, const std::string &name) {
    mailbox.dut.gpo = command(toggle, opcode, index, argument);
    mailbox.dut.eval();
    mailbox.tick();
    toggle = !toggle;
    mailbox.dut.gpo = command(toggle, opcode, index, argument);
    mailbox.dut.eval();
    mailbox.wait_for_ack(toggle, expected, name);
}

void run_fixture_exchange(Mailbox &mailbox, bool &toggle, const Json &item) {
    const std::string name = item.str("name");
    require(item.get("gpo").type == Json::ARR && item.get("gpo").a.size() == 2,
            name + ": gpo pair");
    require(item.get("gpo").a[0].type == Json::NUM && item.get("gpo").a[1].type == Json::NUM,
            name + ": gpo numbers");
    const uint32_t fields = item.get("gpo").a[0].n;
    const uint32_t request = item.get("gpo").a[1].n;
    const uint32_t gpi = item.number("gpi");
    require(bool((fields >> 31) & 1u) == toggle, name + ": field toggle");
    require(request == (fields ^ 0x80000000u), name + ": request toggle only");
    const uint32_t previous_gpi = mailbox.dut.gpi;
    mailbox.dut.gpo = fields;
    mailbox.dut.eval();
    mailbox.tick();
    require(uint32_t(mailbox.dut.gpi) == previous_gpi, name + ": duplicate toggle replayed");
    toggle = !toggle;
    require(bool((request >> 31) & 1u) == toggle, name + ": request toggle");
    mailbox.dut.gpo = request;
    mailbox.dut.eval();
    mailbox.wait_for_ack(toggle, gpi, name);
}

void run_scenario(const Json &scenario) {
    Mailbox mailbox;
    bool toggle = scenario.boolean("initial_request_toggle", false);
    require(!toggle, scenario.str("name") + ": fixture toggle must start false");
    if (scenario.boolean("legacy_active", false)) {
        mailbox.core().media_open = 1;
        mailbox.dut.eval();
    }
    if (!scenario.boolean("initial_reset", true)) {
        mailbox.core().exec_reset = 0;
        mailbox.dut.eval();
    }
    const Json &exchanges = scenario.get("exchanges");
    require(exchanges.type == Json::ARR, scenario.str("name") + ": exchanges");
    for (const auto &item : exchanges.a)
        run_fixture_exchange(mailbox, toggle, item);
    require(bool(mailbox.dut.media_ready) == scenario.boolean("ready", false),
            scenario.str("name") + ": ready");
    require(bool(mailbox.dut.exec_reset) == scenario.boolean("reset", true),
            scenario.str("name") + ": reset");
    require(uint32_t(mailbox.core().stream_received) == scenario.number("received"),
            scenario.str("name") + ": received");
}

void begin_stream(Mailbox &mailbox, bool &toggle, uint32_t total, uint32_t crc,
                  bool expect_ok) {
    exchange(mailbox, toggle, 8, 0, uint16_t(total),
             response(!toggle, false, 0), "begin lo");
    exchange(mailbox, toggle, 8, 1, uint16_t(total >> 16),
             response(!toggle, false, 0), "begin hi");
    exchange(mailbox, toggle, 8, 2, uint16_t(crc),
             response(!toggle, false, 0), "crc lo");
    const uint16_t error = expect_ok ? 0 : 3;
    exchange(mailbox, toggle, 8, 3, uint16_t(crc >> 16),
             response(!toggle, !expect_ok, error), "crc hi / arm");
}

void stream_payload(Mailbox &mailbox, bool &toggle, const std::vector<uint8_t> &payload) {
    size_t offset = 0;
    while (offset < payload.size()) {
        const uint32_t remaining = uint32_t(payload.size() - offset);
        const uint32_t length = remaining > 512 ? 512 : remaining;
        exchange(mailbox, toggle, 9, 0, uint16_t(offset),
                 response(!toggle, false, 0), "chunk lo");
        exchange(mailbox, toggle, 9, 1, uint16_t(offset >> 16),
                 response(!toggle, false, 0), "chunk hi");
        exchange(mailbox, toggle, 9, 2, uint16_t(length),
                 response(!toggle, false, 0), "chunk length");
        uint32_t word = 0;
        uint32_t taken = 0;
        while (taken < length) {
            const uint8_t low = payload[offset + taken];
            uint16_t argument = low;
            if (taken + 1 < length) {
                argument = uint16_t(low | (uint16_t(payload[offset + taken + 1]) << 8));
                taken += 2;
            } else {
                taken += 1;
            }
            exchange(mailbox, toggle, 10, uint8_t(word), argument,
                     response(!toggle, false, 0), "data");
            ++word;
        }
        offset += length;
    }
    exchange(mailbox, toggle, 11, 0, 0, response(!toggle, false, 0), "commit");
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    require(argc == 2, "usage: Vfes_computer_gp STREAM_EXCHANGES_JSON");
    const Json root = read_json(argv[1]);
    require(root.str("interface") == "fes.media.blob-stream", "interface");
    require(root.number("major") == 1 && root.number("minor") == 0, "version");
    require(root.number("capability_bit") == 3, "capability bit");
    require(root.get("endpoint").number("min") == 1, "min");
    require(root.get("endpoint").number("max") == 32768, "max");
    require(root.get("endpoint").number("chunk_max") == 512, "chunk");
    require(root.get("crc_vectors").a.size() == 2, "crc vectors");
    require(root.get("size_vectors").a.size() == 10, "size vectors");
    require(root.get("scenarios").a.size() == 7, "scenarios");

    for (const auto &vector : root.get("crc_vectors").a) {
        const std::string hex = vector.str("hex");
        require(hex.size() % 2 == 0, "crc hex");
        std::vector<uint8_t> bytes(hex.size() / 2);
        for (size_t i = 0; i < bytes.size(); ++i)
            bytes[i] = uint8_t(std::stoul(hex.substr(2 * i, 2), nullptr, 16));
        require(crc32_ieee(bytes.data(), bytes.size()) == vector.number("crc32"),
                "CRC vector " + hex);
    }

    {
        Mailbox mailbox;
        bool toggle = false;
        require(uint32_t(mailbox.dut.gpi) == 0xf5000000u, "initial signature");
        require(mailbox.dut.exec_reset, "initial reset");
        require(!mailbox.dut.media_ready, "initial ready");
        exchange(mailbox, toggle, 1, 7, 0, response(!toggle, false, 0x000f),
                 "capabilities include stream");
        exchange(mailbox, toggle, 1, 4, 0, response(!toggle, false, 2), "abi tag");
        exchange(mailbox, toggle, 4, 0, 3, response(!toggle, false, 0), "legacy begin");
        exchange(mailbox, toggle, 5, 0, 0x0201, response(!toggle, false, 0), "legacy pair");
        exchange(mailbox, toggle, 5, 1, 3, response(!toggle, false, 0), "legacy tail");
        exchange(mailbox, toggle, 6, 0, 0, response(!toggle, false, 0), "legacy commit");
        require(mailbox.dut.media_ready && mailbox.dut.media_size == 3,
                "legacy 3-byte commit");
        require(mailbox.read_media(0) == 1 && mailbox.read_media(1) == 2 &&
                    mailbox.read_media(2) == 3,
                "legacy bytes");
        exchange(mailbox, toggle, 8, 0, 3, response(!toggle, false, 0),
                 "stream begin after committed legacy");
        require(!mailbox.dut.media_ready, "begin clears ready");
        exchange(mailbox, toggle, 12, 0, 0, response(!toggle, false, 0), "abort staged begin");
        exchange(mailbox, toggle, 4, 0, 16385, response(!toggle, true, 3),
                 "legacy 16385 rejected");
    }

    for (const auto &scenario : root.get("scenarios").a)
        run_scenario(scenario);

    for (const auto &vector : root.get("size_vectors").a) {
        Mailbox mailbox;
        bool toggle = false;
        const uint32_t total = vector.number("total");
        const bool accept = vector.boolean("sms_accept", false);
        begin_stream(mailbox, toggle, total, 0, accept);
        if (accept)
            require(mailbox.core().stream_active, "accepted total arms");
        else
            require(!mailbox.core().stream_active && mailbox.core().stream_begin_next == 3,
                    "rejected total does not arm");
    }

    {
        std::vector<uint8_t> payload(32768);
        for (size_t i = 0; i < payload.size(); ++i)
            payload[i] = uint8_t(i);
        const uint32_t crc = crc32_ieee(payload.data(), payload.size());
        Mailbox mailbox;
        bool toggle = false;
        begin_stream(mailbox, toggle, 32768, crc, true);
        exchange(mailbox, toggle, 9, 0, 0, response(!toggle, false, 0), "full offset lo");
        exchange(mailbox, toggle, 9, 1, 0, response(!toggle, false, 0), "full offset hi");
        exchange(mailbox, toggle, 9, 2, 513, response(!toggle, true, 3), "chunk 513");
        exchange(mailbox, toggle, 9, 2, 512, response(!toggle, false, 0), "chunk 512 arm");
        for (uint32_t word = 0; word < 256; ++word) {
            const uint32_t pos = word * 2;
            const uint16_t argument =
                uint16_t(payload[pos] | (uint16_t(payload[pos + 1]) << 8));
            exchange(mailbox, toggle, 10, uint8_t(word), argument,
                     response(!toggle, false, 0), "full chunk word");
        }
        exchange(mailbox, toggle, 10, 0, 0, response(!toggle, true, 4),
                 "ordinal wrap rejected");
        size_t offset = 512;
        while (offset < payload.size()) {
            const uint32_t length = 512;
            exchange(mailbox, toggle, 9, 0, uint16_t(offset),
                     response(!toggle, false, 0), "rest lo");
            exchange(mailbox, toggle, 9, 1, uint16_t(offset >> 16),
                     response(!toggle, false, 0), "rest hi");
            exchange(mailbox, toggle, 9, 2, uint16_t(length),
                     response(!toggle, false, 0), "rest length");
            for (uint32_t word = 0; word < 256; ++word) {
                const uint32_t pos = uint32_t(offset) + word * 2;
                const uint16_t argument =
                    uint16_t(payload[pos] | (uint16_t(payload[pos + 1]) << 8));
                exchange(mailbox, toggle, 10, uint8_t(word), argument,
                         response(!toggle, false, 0), "rest word");
            }
            offset += length;
        }
        exchange(mailbox, toggle, 11, 0, 0, response(!toggle, false, 0), "32768 commit");
        require(mailbox.dut.media_ready && mailbox.dut.media_size == 32768,
                "32768 media_size");
        require(mailbox.read_media(0) == payload[0], "byte 0");
        require(mailbox.read_media(511) == payload[511], "byte 511");
        require(mailbox.read_media(512) == payload[512], "byte 512");
        require(mailbox.read_media(16384) == payload[16384], "byte 16384");
        require(mailbox.read_media(32767) == payload[32767], "byte 32767");
        for (unsigned address = 0; address < payload.size(); address += 257)
            require(mailbox.read_media(uint16_t(address)) == payload[address],
                    "sampled 32768 payload");
    }

    {
        std::vector<uint8_t> long_image(32768, 0x5a);
        long_image[0] = 0x3e;
        long_image[32767] = 0xa5;
        std::vector<uint8_t> short_image{0x01, 0x02, 0x03};
        Mailbox mailbox;
        bool toggle = false;
        begin_stream(mailbox, toggle, uint32_t(long_image.size()),
                     crc32_ieee(long_image.data(), long_image.size()), true);
        stream_payload(mailbox, toggle, long_image);
        require(mailbox.read_media(32767) == 0xa5, "long image tail stored");
        exchange(mailbox, toggle, 2, 0, 0, response(!toggle, false, 0), "hold after long");
        begin_stream(mailbox, toggle, uint32_t(short_image.size()),
                     crc32_ieee(short_image.data(), short_image.size()), true);
        stream_payload(mailbox, toggle, short_image);
        require(mailbox.dut.media_size == 3, "short commit size");
        require(mailbox.read_media(0) == 1 && mailbox.read_media(1) == 2 &&
                    mailbox.read_media(2) == 3,
                "short payload");
    }

    std::cout << "FES SMS blob-stream 1.0 mailbox checks passed\n";
    return EXIT_SUCCESS;
}
