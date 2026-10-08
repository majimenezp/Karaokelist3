package catalog

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func Normalize(value string) string {
	value = norm.NFD.String(strings.ToLower(strings.TrimSpace(value)))
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, value)
}
