/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package custom_metrics

import (
	"fmt"
	"strings"
)

// normalizeSingleSQLStatement removes one terminal statement terminator while
// rejecting separators that could escape a provider's query wrapper.
func normalizeSingleSQLStatement(sql string) (string, error) {
	sql = strings.TrimSpace(sql)
	lastSignificant := -1
	var terminators []int

	for i := 0; i < len(sql); {
		if isSQLWhitespace(sql[i]) {
			i++
			continue
		}
		if strings.HasPrefix(sql[i:], "--") {
			i = skipSQLLineComment(sql, i)
			continue
		}
		if strings.HasPrefix(sql[i:], "/*") {
			end, closed := skipSQLBlockComment(sql, i)
			if !closed {
				return sql, fmt.Errorf("contains unterminated block comment; exactly one SQL statement is allowed")
			}
			i = end
			continue
		}
		if sql[i] == '\'' || sql[i] == '"' {
			end, closed := skipSQLQuoted(sql, i, sql[i], hasBackslashEscapes(sql, i))
			if !closed {
				return sql, fmt.Errorf("contains unterminated quoted value; exactly one SQL statement is allowed")
			}
			lastSignificant = i
			i = end
			continue
		}
		if delimiter, ok := sqlDollarQuoteDelimiter(sql, i); ok {
			closing := strings.Index(sql[i+len(delimiter):], delimiter)
			if closing < 0 {
				return sql, fmt.Errorf("contains unterminated dollar-quoted value; exactly one SQL statement is allowed")
			}
			lastSignificant = i
			i += len(delimiter) + closing + len(delimiter)
			continue
		}

		lastSignificant = i
		if sql[i] == ';' {
			terminators = append(terminators, i)
		}
		i++
	}

	terminal := -1
	if len(terminators) > 0 && terminators[len(terminators)-1] == lastSignificant {
		terminal = terminators[len(terminators)-1]
		terminators = terminators[:len(terminators)-1]
	}
	if len(terminators) > 0 {
		return sql, fmt.Errorf(
			"contains non-terminal statement terminator at byte %d; exactly one SQL statement is allowed",
			terminators[0],
		)
	}
	if terminal < 0 {
		return sql, nil
	}
	return strings.TrimSpace(sql[:terminal] + sql[terminal+1:]), nil
}

func isSQLWhitespace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

func skipSQLLineComment(sql string, start int) int {
	if newline := strings.IndexByte(sql[start+2:], '\n'); newline >= 0 {
		return start + 2 + newline + 1
	}
	return len(sql)
}

func skipSQLBlockComment(sql string, start int) (int, bool) {
	depth := 1
	for i := start + 2; i < len(sql); {
		switch {
		case strings.HasPrefix(sql[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(sql[i:], "*/"):
			depth--
			i += 2
			if depth == 0 {
				return i, true
			}
		default:
			i++
		}
	}
	return len(sql), false
}

func skipSQLQuoted(sql string, start int, quote byte, backslashEscapes bool) (int, bool) {
	for i := start + 1; i < len(sql); {
		if backslashEscapes && sql[i] == '\\' {
			i += 2
			continue
		}
		if sql[i] != quote {
			i++
			continue
		}
		if i+1 < len(sql) && sql[i+1] == quote {
			i += 2
			continue
		}
		return i + 1, true
	}
	return len(sql), false
}

func hasBackslashEscapes(sql string, quote int) bool {
	if quote >= 1 && (sql[quote-1] == 'e' || sql[quote-1] == 'E') &&
		(quote == 1 || !isSQLIdentifierPart(sql[quote-2])) {
		return true
	}
	return quote >= 2 && sql[quote-1] == '&' && (sql[quote-2] == 'u' || sql[quote-2] == 'U') &&
		(quote == 2 || !isSQLIdentifierPart(sql[quote-3]))
}

func sqlDollarQuoteDelimiter(sql string, start int) (string, bool) {
	if sql[start] != '$' || (start > 0 && isSQLIdentifierPart(sql[start-1])) {
		return "", false
	}
	for i := start + 1; i < len(sql); i++ {
		if sql[i] == '$' {
			return sql[start : i+1], true
		}
		if !isSQLDollarTagPart(sql[i], i == start+1) {
			return "", false
		}
	}
	return "", false
}

func isSQLDollarTagPart(ch byte, first bool) bool {
	if ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= 0x80 {
		return true
	}
	return !first && ch >= '0' && ch <= '9'
}

func isSQLIdentifierPart(ch byte) bool {
	return ch == '_' || ch == '$' || ch >= 'a' && ch <= 'z' ||
		ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch >= 0x80
}
