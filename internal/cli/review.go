package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func writerIsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func textDiff(label string, oldText, newText []byte) string {
	oldLines := strings.Split(strings.TrimSuffix(string(oldText), "\n"), "\n")
	newLines := strings.Split(strings.TrimSuffix(string(newText), "\n"), "\n")
	if len(oldText) == 0 {
		oldLines = nil
	}
	if len(newText) == 0 {
		newLines = nil
	}
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix && oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s (locked)\n+++ %s (fetched)\n", label, label)
	contextStart := prefix - 2
	if contextStart < 0 {
		contextStart = 0
	}
	for _, line := range oldLines[contextStart:prefix] {
		fmt.Fprintf(&out, " %s\n", line)
	}
	for _, line := range oldLines[prefix : len(oldLines)-suffix] {
		fmt.Fprintf(&out, "-%s\n", line)
	}
	for _, line := range newLines[prefix : len(newLines)-suffix] {
		fmt.Fprintf(&out, "+%s\n", line)
	}
	if suffix > 0 {
		end := suffix
		if end > 2 {
			end = 2
		}
		for _, line := range newLines[len(newLines)-suffix : len(newLines)-suffix+end] {
			fmt.Fprintf(&out, " %s\n", line)
		}
	}
	return out.String()
}

func surfaceText(root string) []byte {
	var names []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if rel, relErr := filepath.Rel(root, path); relErr == nil {
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(names)
	var out bytes.Buffer
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || bytes.IndexByte(body, 0) >= 0 {
			continue
		}
		fmt.Fprintf(&out, "== %s ==\n", name)
		if len(body) > 1<<20 {
			body = body[:1<<20]
		}
		out.Write(body)
		if len(body) == 0 || body[len(body)-1] != '\n' {
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}
