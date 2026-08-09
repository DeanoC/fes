package stagea0

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

func ApplyVDateRecipeV1(source []byte, spec VDateSpec) ([]byte, error) {
	if err := validateRecipeV1(spec); err != nil {
		return nil, err
	}
	if err := verifySourceBytes(source, spec.SourceEvidenceSHA256); err != nil {
		return nil, err
	}
	lineStart, tokenStart, tokenEnd, lineEnd, count := recipeV1Offsets(source, recipeV1Token(spec))
	if count != 1 {
		return nil, failure(CodeVDateRecipeMismatch, "recipe", "date command token is not unique")
	}
	out := make([]byte, 0, len(source)+len(RecipeV1ValidationBlock)-tokenEnd+tokenStart+len(RecipeV1DateValue))
	out = append(out, source[:lineStart]...)
	out = append(out, RecipeV1ValidationBlock...)
	out = append(out, source[lineStart:tokenStart]...)
	out = append(out, RecipeV1DateValue...)
	out = append(out, source[tokenEnd:lineEnd]...)
	return append(out, source[lineEnd:]...), nil
}

func validateRecipeV1(spec VDateSpec) error {
	if !safeSourcePath(spec.SourcePath) || !lowerHex(spec.SourceEvidenceSHA256, 64) || spec.OfficialExpression != VDateExpressionV1 || spec.Format != VDateFormatV1 || spec.Timezone != VDateTimezoneV1 || spec.ASCIIDigits != VDateDigitsV1 {
		return failure(CodeVDateRecipeMismatch, "recipe", "Recipe V1 specification is invalid")
	}
	return nil
}

func verifySourceBytes(source []byte, expectedSHA256 string) error {
	if !utf8.Valid(source) || len(source) == 0 || source[len(source)-1] != '\n' || bytes.IndexByte(source, '\r') >= 0 {
		return failure(CodeVDateRecipeMismatch, "recipe", "source bytes must be UTF-8, LF-only, and LF-terminated")
	}
	digest := sha256.Sum256(source)
	if hex.EncodeToString(digest[:]) != expectedSHA256 {
		return failure(CodeVDateRecipeMismatch, "recipe", "source evidence hash does not match")
	}
	return nil
}

func recipeV1Token(spec VDateSpec) []byte {
	return []byte("`date +\"" + spec.OfficialExpression + "\"`")
}

func recipeV1Offsets(source, token []byte) (lineStart, tokenStart, tokenEnd, lineEnd, count int) {
	for offset := 0; ; {
		i := bytes.Index(source[offset:], token)
		if i < 0 {
			break
		}
		tokenStart = offset + i
		tokenEnd = tokenStart + len(token)
		count++
		offset = tokenEnd
	}
	if count != 1 {
		return 0, 0, 0, 0, count
	}
	lineStart = strings.LastIndex(string(source[:tokenStart]), "\n") + 1
	lineEnd = tokenEnd + bytes.IndexByte(source[tokenEnd:], '\n') + 1
	return lineStart, tokenStart, tokenEnd, lineEnd, count
}
