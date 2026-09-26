// Copyright 2026 FogCast contributors
// SPDX-License-Identifier: GPL-3.0-or-later

#include "daemon/json.hpp"

#include <limits>

namespace mister {
namespace daemon {
namespace json {
namespace {

const std::size_t kMaximumInputBytes = 65536;
const int kMaximumObjectLevels = 4;

class Parser {
public:
	Parser(const std::string& input, std::string* message)
		: input_(input), position_(0), message_(message) {}

	bool ParseDocument(Value* value)
	{
		if (input_.size() > kMaximumInputBytes)
			return Fail("request exceeds 65536 bytes");
		SkipWhitespace();
		if (!ParseValue(0, value)) return false;
		SkipWhitespace();
		return position_ == input_.size() || Fail("trailing data");
	}

private:
	bool Fail(const char* message)
	{
		if (message_ != nullptr) *message_ = message;
		return false;
	}

	void SkipWhitespace()
	{
		while (position_ < input_.size()) {
			const char value = input_[position_];
			if (value != ' ' && value != '\n' && value != '\r' && value != '\t') break;
			++position_;
		}
	}

	bool ParseValue(int object_level, Value* value)
	{
		if (position_ == input_.size()) return Fail("missing value");
		const char token = input_[position_];
		if (token == '{') return ParseObject(object_level + 1, value);
		if (token == '"') {
			value->type = Type::string;
			return ParseString(&value->string_value);
		}
		if (token == '-' || (token >= '0' && token <= '9')) {
			value->type = Type::integer;
			return ParseInteger(value);
		}
		if (token == 't') {
			if (!Consume("true")) return Fail("invalid token");
			value->type = Type::boolean;
			value->boolean_value = true;
			return true;
		}
		if (token == 'f') {
			if (!Consume("false")) return Fail("invalid token");
			value->type = Type::boolean;
			value->boolean_value = false;
			return true;
		}
		if (token == 'n') {
			if (!Consume("null")) return Fail("invalid token");
			value->type = Type::null_value;
			return true;
		}
		if (token == '[') return ParseArray(object_level + 1, value);
		return Fail("invalid value");
	}

	bool ParseObject(int object_level, Value* value)
	{
		if (object_level > kMaximumObjectLevels)
			return Fail("object nesting exceeds four levels");
		++position_;
		value->type = Type::object;
		value->object.clear();
		SkipWhitespace();
		if (ConsumeCharacter('}')) return true;
		while (true) {
			if (position_ == input_.size() || input_[position_] != '"')
				return Fail("object key must be a string");
			std::string key;
			if (!ParseString(&key)) return false;
			for (const auto& member : value->object) {
				if (member.first == key) return Fail("duplicate object key");
			}
			SkipWhitespace();
			if (!ConsumeCharacter(':')) return Fail("missing object colon");
			SkipWhitespace();
			Value member;
			if (!ParseValue(object_level, &member)) return false;
			value->object.push_back(std::make_pair(key, member));
			SkipWhitespace();
			if (ConsumeCharacter('}')) return true;
			if (!ConsumeCharacter(',')) return Fail("missing object comma");
			SkipWhitespace();
		}
	}

	bool ParseArray(int level, Value* value)
	{
		if (level > kMaximumObjectLevels) return Fail("JSON nesting exceeds four levels");
		++position_;
		value->type = Type::array;
		value->array.clear();
		SkipWhitespace();
		if (ConsumeCharacter(']')) return true;
		while (true) {
			Value member;
			if (!ParseValue(level, &member)) return false;
			value->array.push_back(std::move(member));
			SkipWhitespace();
			if (ConsumeCharacter(']')) return true;
			if (!ConsumeCharacter(',')) return Fail("missing array comma");
			SkipWhitespace();
		}
	}

	bool ParseString(std::string* output)
	{
		if (!ConsumeCharacter('"')) return Fail("missing string quote");
		output->clear();
		while (position_ < input_.size()) {
			const unsigned char byte = static_cast<unsigned char>(input_[position_]);
			if (byte == '"') {
				++position_;
				return true;
			}
			if (byte < 0x20) return Fail("unescaped control character");
			if (byte == '\\') {
				++position_;
				if (!ParseEscape(output)) return false;
				continue;
			}
			if (byte < 0x80) {
				output->push_back(static_cast<char>(byte));
				++position_;
				continue;
			}
			std::size_t count = 0;
			if (!Utf8Sequence(position_, &count)) return false;
			output->append(input_, position_, count);
			position_ += count;
		}
		return Fail("unterminated string");
	}

	bool ParseEscape(std::string* output)
	{
		if (position_ == input_.size()) return Fail("unterminated escape");
		const char escape = input_[position_++];
		switch (escape) {
		case '"': output->push_back('"'); return true;
		case '\\': output->push_back('\\'); return true;
		case '/': output->push_back('/'); return true;
		case 'b': output->push_back('\b'); return true;
		case 'f': output->push_back('\f'); return true;
		case 'n': output->push_back('\n'); return true;
		case 'r': output->push_back('\r'); return true;
		case 't': output->push_back('\t'); return true;
		case 'u': return ParseUnicodeEscape(output);
		default: return Fail("invalid escape");
		}
	}

	bool ParseUnicodeEscape(std::string* output)
	{
		std::uint32_t codepoint = 0;
		if (!ParseHexCodepoint(&codepoint)) return false;
		if (codepoint >= 0xd800 && codepoint <= 0xdbff) {
			if (position_ + 2 > input_.size() || input_[position_] != '\\' ||
				input_[position_ + 1] != 'u') return Fail("unpaired high surrogate");
			position_ += 2;
			std::uint32_t low = 0;
			if (!ParseHexCodepoint(&low)) return false;
			if (low < 0xdc00 || low > 0xdfff) return Fail("invalid surrogate pair");
			codepoint = 0x10000 + ((codepoint - 0xd800) << 10) + (low - 0xdc00);
		} else if (codepoint >= 0xdc00 && codepoint <= 0xdfff) {
			return Fail("unpaired low surrogate");
		}
	if (codepoint <= 0x7f) {
			output->push_back(static_cast<char>(codepoint));
		} else if (codepoint <= 0x7ff) {
			output->push_back(static_cast<char>(0xc0 | (codepoint >> 6)));
			output->push_back(static_cast<char>(0x80 | (codepoint & 0x3f)));
		} else if (codepoint <= 0xffff) {
			output->push_back(static_cast<char>(0xe0 | (codepoint >> 12)));
			output->push_back(static_cast<char>(0x80 | ((codepoint >> 6) & 0x3f)));
			output->push_back(static_cast<char>(0x80 | (codepoint & 0x3f)));
		} else {
			output->push_back(static_cast<char>(0xf0 | (codepoint >> 18)));
			output->push_back(static_cast<char>(0x80 | ((codepoint >> 12) & 0x3f)));
			output->push_back(static_cast<char>(0x80 | ((codepoint >> 6) & 0x3f)));
			output->push_back(static_cast<char>(0x80 | (codepoint & 0x3f)));
		}
		return true;
	}

	bool ParseHexCodepoint(std::uint32_t* value)
	{
		if (position_ + 4 > input_.size()) return Fail("short unicode escape");
		*value = 0;
		for (int index = 0; index < 4; ++index) {
			const char digit = input_[position_++];
			*value <<= 4;
			if (digit >= '0' && digit <= '9') *value |= digit - '0';
			else if (digit >= 'a' && digit <= 'f') *value |= digit - 'a' + 10;
			else if (digit >= 'A' && digit <= 'F') *value |= digit - 'A' + 10;
			else return Fail("invalid unicode escape");
		}
		return true;
	}

	bool Utf8Sequence(std::size_t start, std::size_t* count)
	{
		const unsigned char first = static_cast<unsigned char>(input_[start]);
		if (first >= 0xc2 && first <= 0xdf) *count = 2;
		else if (first >= 0xe0 && first <= 0xef) *count = 3;
		else if (first >= 0xf0 && first <= 0xf4) *count = 4;
		else return Fail("invalid UTF-8");
		if (start + *count > input_.size()) return Fail("truncated UTF-8");
		for (std::size_t index = 1; index < *count; ++index) {
			const unsigned char continuation = static_cast<unsigned char>(input_[start + index]);
			if (continuation < 0x80 || continuation > 0xbf) return Fail("invalid UTF-8");
		}
		const unsigned char second = static_cast<unsigned char>(input_[start + 1]);
		if ((first == 0xe0 && second < 0xa0) || (first == 0xed && second > 0x9f) ||
			(first == 0xf0 && second < 0x90) || (first == 0xf4 && second > 0x8f))
			return Fail("invalid UTF-8");
		return true;
	}

	bool ParseInteger(Value* value)
	{
		const bool negative = ConsumeCharacter('-');
		if (position_ == input_.size() || input_[position_] < '0' || input_[position_] > '9')
			return Fail("invalid integer");
		if (input_[position_] == '0' && position_ + 1 < input_.size() &&
			input_[position_ + 1] >= '0' && input_[position_ + 1] <= '9')
			return Fail("leading zero");
		const std::uint64_t limit = negative
			? static_cast<std::uint64_t>(std::numeric_limits<std::int64_t>::max()) + 1
			: std::numeric_limits<std::uint64_t>::max();
		std::uint64_t parsed = 0;
		while (position_ < input_.size() && input_[position_] >= '0' && input_[position_] <= '9') {
			const std::uint64_t digit = static_cast<std::uint64_t>(input_[position_] - '0');
			if (parsed > (limit - digit) / 10) return Fail("integer out of range");
			parsed = parsed * 10 + digit;
			++position_;
		}
		if (negative) {
			if (parsed == limit) value->integer_value = std::numeric_limits<std::int64_t>::min();
			else value->integer_value = -static_cast<std::int64_t>(parsed);
		} else if (parsed > static_cast<std::uint64_t>(std::numeric_limits<std::int64_t>::max())) {
			value->type = Type::unsigned_integer;
			value->unsigned_value = parsed;
		} else {
			value->integer_value = static_cast<std::int64_t>(parsed);
		}
		return true;
	}

	bool Consume(const char* token)
	{
		std::size_t index = 0;
		while (token[index] != '\0') {
			if (position_ + index >= input_.size() || input_[position_ + index] != token[index])
				return false;
			++index;
		}
		position_ += index;
		return true;
	}

	bool ConsumeCharacter(char expected)
	{
		if (position_ == input_.size() || input_[position_] != expected) return false;
		++position_;
		return true;
	}

	const std::string& input_;
	std::size_t position_;
	std::string* message_;
};

} // namespace

bool Parse(const std::string& input, Value* value, std::string* message)
{
	if (value == nullptr) {
		if (message != nullptr) *message = "missing output value";
		return false;
	}
	return Parser(input, message).ParseDocument(value);
}

} // namespace json
} // namespace daemon
} // namespace mister
