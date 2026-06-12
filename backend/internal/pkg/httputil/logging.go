package httputil

import "strings"

const maxLogHeaderValueLen = 256

// SanitizeHeaderValueForLog normalizes request header values before they are
// written to structured logs.
func SanitizeHeaderValueForLog(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > maxLogHeaderValueLen {
		value = value[:maxLogHeaderValueLen]
	}
	return value
}
