package declared

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/manifest"
	"github.com/neprel/git-a2a/internal/remotehttp"
)

type Driver struct {
	ContactKind string
	Consent     *manifest.ContactSettings
	MCP         bool
	Client      *http.Client
	LookPath    func(string) (string, error)
	Run         func(context.Context, string, []string, string) ([]byte, error)
}

func (d Driver) Kind() string { return d.ContactKind }

func (d Driver) Deliver(ctx context.Context, request contact.Request) (contact.Record, error) {
	switch d.ContactKind {
	case "http":
		return d.deliverHTTP(ctx, request)
	case "exec":
		return d.deliverExec(ctx, request)
	default:
		return contact.Record{}, fmt.Errorf("declared invocation: unsupported contact kind %s", d.ContactKind)
	}
}

func (d Driver) deliverHTTP(ctx context.Context, request contact.Request) (contact.Record, error) {
	finalURL, err := expandURL(request.Contact.URL, request)
	if err != nil {
		return contact.Record{}, fmt.Errorf("http contact: URL: %w", err)
	}
	body := expandBody(request.Contact.Body, request.Contact.ContentType, request)
	method := strings.ToUpper(strings.TrimSpace(request.Contact.Method))
	if method == "" {
		method = http.MethodPost
	}
	headers := finalHeaders(request.Contact.Headers, request.Contact.ContentType)
	instruction := curlInstruction(method, finalURL, headers, body)
	if request.DryRun || !contains(d.allowedHTTP(), origin(finalURL)) {
		return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "instruction", ID: finalURL, State: "instruction", Instruction: instruction}, nil
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, finalURL, strings.NewReader(body))
	if err != nil {
		return contact.Record{}, fmt.Errorf("http contact: %w", err)
	}
	for name, value := range headers {
		httpRequest.Header.Set(name, value)
	}
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return contact.Record{}, fmt.Errorf("http contact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contact.Record{}, fmt.Errorf("http contact: %s", remotehttp.ErrorResponse(response))
	}
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	id := sanitizeExcerpt(string(responseBody))
	if id == "" {
		id = response.Status
	}
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "http", ID: id, State: "sent"}, nil
}

func (d Driver) deliverExec(ctx context.Context, request contact.Request) (contact.Record, error) {
	if d.MCP {
		return contact.Record{}, fmt.Errorf("exec contact: refused through MCP; run git-a2a contact from an approved CLI session")
	}
	command := append([]string(nil), request.Contact.Command...)
	command = append(command, request.Contact.Args...)
	for i := range command {
		command[i] = expandPlain(command[i], request, false)
	}
	stdin := request.Contact.Stdin
	if stdin == "" {
		stdin = "{message}"
	}
	stdin = expandPlain(stdin, request, true)
	instruction := argvInstruction(command, stdin)
	if len(command) == 0 {
		return contact.Record{}, fmt.Errorf("exec contact: command is empty")
	}
	if request.DryRun || !contains(d.allowedExec(), command[0]) {
		return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "instruction", ID: command[0], State: "instruction", Instruction: instruction}, nil
	}
	lookPath := d.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	executable, err := lookPath(command[0])
	if err != nil {
		return contact.Record{}, fmt.Errorf("exec contact: %s is allowed but not found on PATH", command[0])
	}
	run := d.Run
	if run == nil {
		run = runCommand
	}
	output, err := run(ctx, executable, command[1:], stdin)
	if err != nil {
		return contact.Record{}, fmt.Errorf("exec contact: %w", err)
	}
	id := sanitizeExcerpt(string(output))
	if id == "" {
		id = command[0]
	}
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "exec:" + command[0], ID: id, State: "sent"}, nil
}

func expandURL(raw string, request contact.Request) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, values := range query {
		for i, value := range values {
			values[i] = expandPlain(value, request, false)
		}
		query[key] = values
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func expandBody(template, contentType string, request contact.Request) string {
	if strings.Contains(strings.ToLower(contentType), "json") {
		return expandJSON(template, request)
	}
	return expandPlain(template, request, true)
}

func expandJSON(value string, request contact.Request) string {
	for placeholder, replacement := range replacements(request, true) {
		encoded, _ := json.Marshal(replacement)
		escaped := string(encoded[1 : len(encoded)-1])
		value = strings.ReplaceAll(value, placeholder, escaped)
	}
	return value
}

func expandPlain(value string, request contact.Request, message bool) string {
	for placeholder, replacement := range replacements(request, message) {
		value = strings.ReplaceAll(value, placeholder, replacement)
	}
	return value
}

func replacements(request contact.Request, message bool) map[string]string {
	values := map[string]string{"{intent}": request.Intent, "{module}": request.Module, "{origin}": request.Origin}
	if message {
		values["{message}"] = request.Message
	}
	return values
}

func curlInstruction(method, target string, headers map[string]string, body string) string {
	parts := []string{"curl", "-X", shellQuote(method), shellQuote(target)}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := headers[name]
		parts = append(parts, "-H", shellQuote(name+": "+value))
	}
	if body != "" {
		parts = append(parts, "--data-binary", shellQuote(body))
	}
	return strings.Join(parts, " ")
}

func finalHeaders(declared map[string]string, contentType string) map[string]string {
	result := make(map[string]string, len(declared)+1)
	for name, value := range declared {
		if contentType != "" && strings.EqualFold(name, "Content-Type") {
			continue
		}
		result[name] = value
	}
	if contentType != "" {
		result["Content-Type"] = contentType
	}
	return result
}

func argvInstruction(argv []string, stdin string) string {
	parts := make([]string, len(argv))
	for i, value := range argv {
		parts[i] = strconv.Quote(value)
	}
	result := "argv " + strings.Join(parts, " ")
	if stdin != "" {
		result += " with stdin " + strconv.Quote(stdin)
	}
	return result
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func origin(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (d Driver) allowedHTTP() []string {
	if d.Consent == nil {
		return nil
	}
	return d.Consent.AllowHTTP
}

func (d Driver) allowedExec() []string {
	if d.Consent == nil {
		return nil
	}
	return d.Consent.AllowExec
}

func runCommand(ctx context.Context, executable string, args []string, input string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, sanitizeExcerpt(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func sanitizeExcerpt(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 200 {
		runes = runes[:200]
	}
	return string(runes)
}
