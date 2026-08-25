package email

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestSendmailReceivesRFC5322Message(t *testing.T) {
	var executable, input string
	var args []string
	driver := Driver{
		LookPath: func(string) (string, error) { return "/usr/sbin/sendmail", nil },
		Run: func(_ context.Context, command string, commandArgs []string, stdin string) error {
			executable, args, input = command, commandArgs, stdin
			return nil
		},
	}
	record, err := driver.Deliver(context.Background(), request())
	if err != nil || record.Driver != "sendmail" || record.State != "sent" {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	if executable != "/usr/sbin/sendmail" || strings.Join(args, " ") != "-i -t" || !strings.Contains(input, "To: owner@example.com\r\nSubject: [acme] Change the API") || !strings.Contains(input, "\r\n\r\nChange the API\r\nSecond line") {
		t.Fatalf("command=%s %v input=%q", executable, args, input)
	}
}

func TestSMTPDeliveryAgainstFakeServer(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	done := make(chan error, 1)
	var delivered string
	go func() {
		defer serverSide.Close()
		reader := bufio.NewReader(serverSide)
		write := func(line string) { _, _ = fmt.Fprintf(serverSide, "%s\r\n", line) }
		write("220 smtp.acme.test ESMTP")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- err
				return
			}
			upper := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(upper, "EHLO"):
				write("250-smtp.acme.test")
				write("250 AUTH PLAIN")
			case strings.HasPrefix(upper, "AUTH PLAIN"):
				write("235 authenticated")
			case strings.HasPrefix(upper, "MAIL FROM") || strings.HasPrefix(upper, "RCPT TO"):
				write("250 ok")
			case upper == "DATA":
				write("354 send data")
				var body strings.Builder
				for {
					dataLine, readErr := reader.ReadString('\n')
					if readErr != nil {
						done <- readErr
						return
					}
					if dataLine == ".\r\n" {
						break
					}
					body.WriteString(dataLine)
				}
				delivered = body.String()
				write("250 queued as acme-1")
			case upper == "QUIT":
				write("221 bye")
				done <- nil
				return
			}
		}
	}()
	driver := Driver{
		LookPath: func(string) (string, error) { return "", fmt.Errorf("not found") },
		Getenv: func(key string) string {
			if key == "GITA2A_SMTP_URL" {
				return "smtps://sender@smtp.acme.test:465"
			}
			if key == "GITA2A_SMTP_PASSWORD" {
				return "consumer-secret"
			}
			return ""
		},
		DialTLS: func(context.Context, string, *tls.Config) (net.Conn, error) { return clientSide, nil },
		Auth:    func(_, user, password string) smtp.Auth { return testAuth{user: user, password: password} },
	}
	record, err := driver.Deliver(context.Background(), request())
	if err != nil || record.Driver != "smtp" || record.State != "sent" {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(delivered, "To: owner@example.com") || !strings.Contains(delivered, "Second line") {
		t.Fatalf("delivered=%q", delivered)
	}
}

type testAuth struct{ user, password string }

func (a testAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.password), nil
}
func (testAuth) Next([]byte, bool) ([]byte, error) { return nil, nil }

func TestNoTransportFallsBackToInstruction(t *testing.T) {
	driver := Driver{LookPath: func(string) (string, error) { return "", fmt.Errorf("not found") }, Getenv: func(string) string { return "" }}
	record, err := driver.Deliver(context.Background(), request())
	if err != nil || record.State != "instruction" || record.Driver != "instruction" {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

func request() contact.Request {
	return contact.Request{Agent: "acme-owner", Intent: "change", Module: "acme-lib", Message: "Change the API\nSecond line", Contact: manifest.Contact{Kind: "email", Address: "owner@example.com", SubjectPrefix: "[acme]"}}
}
