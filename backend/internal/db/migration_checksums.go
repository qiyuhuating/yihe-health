package db

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

// Old binaries hashed embedded SQL bytes, which Git may check out as LF or
// CRLF. Preserve their ledger entries and fresh-write format, accepting only
// these newline variants outside literals. Other SQL changes still fail closed.
func migrationChecksumMatches(data []byte, recorded string) (bool, error) {
	if recorded == fmt.Sprintf("%x", sha256.Sum256(data)) {
		return true, nil
	}
	for _, crlf := range []bool{false, true} {
		variant, err := migrationLineEndings(string(data), crlf)
		if err != nil {
			return false, err
		}
		if recorded == fmt.Sprintf("%x", sha256.Sum256([]byte(variant))) {
			return true, nil
		}
	}
	return false, nil
}

func lineEndings(text string, crlf bool) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

func sqlCommentSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func migrationLineEndings(query string, crlf bool) (string, error) {
	var out strings.Builder
	out.Grow(len(query))
	for i := 0; i < len(query); {
		if query[i] == '\'' || query[i] == '"' || query[i] == '`' {
			end, err := quotedSQLEnd(query, i)
			if err != nil {
				return "", err
			}
			out.WriteString(query[i:end])
			i = end
			continue
		}
		if strings.HasPrefix(query[i:], "/*") {
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				return "", errors.New("unterminated migration SQL block comment")
			}
			end += i + 4
			token := query[i:end]
			// Executable comments and optimizer hints may contain SQL tokens.
			// Keep their contents byte-for-byte rather than treating them as prose.
			if strings.HasPrefix(token, "/*!") || strings.HasPrefix(token, "/*+") {
				out.WriteString(token)
			} else {
				out.WriteString(lineEndings(token, crlf))
			}
			i = end
			continue
		}
		lineComment := query[i] == '#' || (strings.HasPrefix(query[i:], "--") && (i+2 == len(query) || sqlCommentSpace(query[i+2])))
		if lineComment {
			end := strings.IndexAny(query[i:], "\r\n")
			if end < 0 {
				end = len(query)
			} else {
				end += i + 1
				if query[end-1] == '\r' && end < len(query) && query[end] == '\n' {
					end++
				}
			}
			out.WriteString(lineEndings(query[i:end], crlf))
			i = end
			continue
		}
		if query[i] == '\r' && i+1 < len(query) && query[i+1] == '\n' {
			if crlf {
				out.WriteByte('\r')
			}
			out.WriteByte('\n')
			i += 2
			continue
		}
		if query[i] == '\n' {
			if crlf {
				out.WriteByte('\r')
			}
			out.WriteByte('\n')
			i++
			continue
		}
		out.WriteByte(query[i])
		i++
	}
	return out.String(), nil
}

func quotedSQLEnd(query string, start int) (int, error) {
	quote := query[start]
	for i := start + 1; i < len(query); i++ {
		switch query[i] {
		case '\\':
			i++
		case quote:
			if i+1 == len(query) || query[i+1] != quote {
				return i + 1, nil
			}
			i++
		}
	}
	return 0, errors.New("unterminated migration SQL quoted value")
}
