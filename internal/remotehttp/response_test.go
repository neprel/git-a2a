package remotehttp

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestErrorResponseSuppressesHTML(t *testing.T) {
	for _, test := range []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "markup", body: "  \n<html><secret></html>"},
		{name: "content type", contentType: "text/html; charset=utf-8", body: "secret"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := testResponse(http.StatusForbidden, test.contentType, test.body)
			want := fmt.Sprintf("HTTP 403 Forbidden (html response, %d bytes, suppressed)", len(test.body))
			if got := ErrorResponse(response); got != want {
				t.Fatalf("ErrorResponse() = %q, want %q", got, want)
			}
		})
	}
}

func TestErrorResponseSanitizesTextAndBoundsExcerpt(t *testing.T) {
	response := testResponse(http.StatusBadRequest, "application/json", "  one\x00\r\n two  "+strings.Repeat("x", 240))
	got := ErrorResponse(response)
	const prefix = `HTTP 400 Bad Request: one two `
	if !strings.HasPrefix(got, prefix) || strings.ContainsAny(got, "\x00\r\n") {
		t.Fatalf("ErrorResponse() = %q", got)
	}
	if excerpt := strings.TrimPrefix(got, "HTTP 400 Bad Request: "); len([]rune(excerpt)) != 200 {
		t.Fatalf("excerpt length = %d, want 200", len([]rune(excerpt)))
	}
}

func testResponse(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        http.Header{"Content-Type": []string{contentType}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}
