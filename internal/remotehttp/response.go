package remotehttp

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
)

const (
	maxRead    = 4096
	maxExcerpt = 200
)

// ErrorResponse formats an untrusted non-success HTTP response without
// allowing markup or control characters into a terminal error.
func ErrorResponse(response *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxRead))
	size := int64(len(body))
	if response.ContentLength >= 0 {
		size = response.ContentLength
	}
	trimmed := strings.TrimSpace(string(body))
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.HasPrefix(trimmed, "<") || strings.Contains(contentType, "text/html") {
		return fmt.Sprintf("HTTP %s (html response, %d bytes, suppressed)", response.Status, size)
	}
	excerpt := sanitize(trimmed)
	if excerpt == "" {
		return "HTTP " + response.Status
	}
	return fmt.Sprintf("HTTP %s: %s", response.Status, excerpt)
}

func sanitize(value string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	clean = strings.Join(strings.Fields(clean), " ")
	runes := []rune(clean)
	if len(runes) > maxExcerpt {
		runes = runes[:maxExcerpt]
	}
	return string(runes)
}
