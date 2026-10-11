// SPDX-License-Identifier: GPL-2.0-or-later
// STF LowPassFilter and DC arithmetic from Hatari v2.5.0, GPL-2.0-or-later:
// https://github.com/hatari/hatari/blob/v2.5.0/src/sound.c
// Primary SHA256: 2ffdf73c27c82edd998c41a250cd424a8da4f7385d8059f21d36bdc49c99c4f0
// src/audio.c selects LowPassFilter for ST output at 40 kHz and above.
// Exact-rational source integration replaces Hatari's truncated Q16 interval.
// This is a mathematical oracle, independent of the RTL pipeline/divider/FIFO.
#ifndef ST_YM_FILTER_REFERENCE_HPP
#define ST_YM_FILTER_REFERENCE_HPP
#include <algorithm>
#include <cstdint>
#include <deque>
#include <limits>
#include <stdexcept>

namespace st_ym_filter_reference {
class Reference {
    const uint64_t interval_, step_;
    uint64_t weight_ = 0, integral_ = 0;
    unsigned previous_input_ = 0, low_pass_ = 0;
    int32_t dc_state_ = 0, previous_average_ = 0, previous_output_ = 0;
    std::deque<unsigned> averages_;
public:
    explicit Reference(unsigned chip_clock = 2005311, unsigned sample_rate = 48000)
        : interval_(chip_clock), step_(uint64_t(8) * sample_rate)
    {
        if (!step_ || step_ >= interval_)
            throw std::invalid_argument("filter requires 0 < 8*sample_rate < chip_clock");
        reset();
    }
    void reset()
    {
        // One initial zero counter interval precedes the first input interval.
        weight_ = step_; integral_ = 0;
        previous_input_ = low_pass_ = 0;
        dc_state_ = previous_average_ = previous_output_ = 0;
        averages_.clear();
    }
    void input(unsigned raw)
    {
        if (raw > 32767) throw std::out_of_range("15-bit filter input");
        low_pass_ = raw >= low_pass_ ?
            (3 * (raw + previous_input_) + 2 * low_pass_) / 8 :
            ((raw + previous_input_) + 6 * low_pass_) / 8;
        previous_input_ = raw;
        uint64_t remaining = step_;
        while (remaining) {
            const uint64_t portion = std::min(remaining, interval_ - weight_);
            integral_ += uint64_t(low_pass_) * portion;
            weight_ += portion;
            remaining -= portion;
            if (weight_ == interval_) {
                averages_.push_back(unsigned(integral_ / interval_));
                weight_ = integral_ = 0;
            }
        }
    }
    int16_t output()
    {
        if (averages_.empty()) throw std::underflow_error("no completed target interval");
        const int32_t x = int32_t(averages_.front());
        averages_.pop_front();
        const int64_t next = int64_t(dc_state_) + int64_t(x - previous_average_) * 32768
                             - int64_t(previous_output_) * 64;
        if (next < std::numeric_limits<int32_t>::min() || next > std::numeric_limits<int32_t>::max())
            throw std::overflow_error("DC state exceeded signed 32 bits");
        dc_state_ = int32_t(next);
        // Explicit floor division implements an arithmetic right shift for negatives.
        previous_output_ = int32_t(next >= 0 ? next / 32768 : -((-next + 32767) / 32768));
        previous_average_ = x;
        if (previous_output_ < -32768 || previous_output_ > 32767)
            throw std::overflow_error("DC output exceeded signed 16 bits");
        return int16_t(previous_output_);
    }
    std::size_t pending() const { return averages_.size(); }
    unsigned low_pass() const { return low_pass_; }
};
} // namespace st_ym_filter_reference
#endif
