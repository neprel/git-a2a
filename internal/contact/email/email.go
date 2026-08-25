package email

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/contact/forge"
	"github.com/neprel/git-a2a/internal/contact/instruction"
)

type Driver struct {
	LookPath func(string) (string, error)
	Run      func(context.Context, string, []string, string) error
	Getenv   func(string) string
	DialTLS  func(context.Context, string, *tls.Config) (net.Conn, error)
	Auth     func(host, user, password string) smtp.Auth
}

func (Driver) Kind() string { return "email" }

func (d Driver) Deliver(ctx context.Context, request contact.Request) (contact.Record, error) {
	if request.DryRun {
		return instruction.Driver{ContactKind: d.Kind()}.Deliver(ctx, request)
	}
	recipient, err := mail.ParseAddress(request.Contact.Address)
	if err != nil || recipient.Address == "" {
		return contact.Record{}, fmt.Errorf("email: invalid address %q", request.Contact.Address)
	}
	message := rfc5322(recipient.Address, request.Contact.SubjectPrefix, request.Message)
	lookPath := d.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if executable, err := lookPath("sendmail"); err == nil {
		run := d.Run
		if run == nil {
			run = runSendmail
		}
		if err := run(ctx, executable, []string{"-i", "-t"}, message); err != nil {
			return contact.Record{}, fmt.Errorf("email: sendmail: %w", err)
		}
		return sent(request, recipient.Address, "sendmail"), nil
	}
	getenv := d.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	endpoint := getenv("GITA2A_SMTP_URL")
	if endpoint == "" {
		return instruction.Driver{ContactKind: d.Kind()}.Deliver(ctx, request)
	}
	if err := d.sendSMTP(ctx, endpoint, getenv("GITA2A_SMTP_PASSWORD"), recipient.Address, message); err != nil {
		return contact.Record{}, err
	}
	return sent(request, recipient.Address, "smtp"), nil
}

func sent(request contact.Request, address, driver string) contact.Record {
	return contact.Record{Agent: request.Agent, Kind: "email", Driver: driver, ID: address, State: "sent"}
}

func rfc5322(to, prefix, body string) string {
	prefix = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, prefix)
	subject := strings.TrimSpace(strings.TrimSpace(prefix) + " " + forge.Title(body))
	return "To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
}

func runSendmail(ctx context.Context, executable string, args []string, input string) error {
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (d Driver) sendSMTP(ctx context.Context, raw, password, to, message string) error {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme != "smtps" || endpoint.Hostname() == "" || endpoint.User == nil {
		return fmt.Errorf("email: GITA2A_SMTP_URL must be smtps://user@host[:port]")
	}
	user := endpoint.User.Username()
	if user == "" {
		return fmt.Errorf("email: GITA2A_SMTP_URL must include a user")
	}
	if password == "" {
		return fmt.Errorf("email: GITA2A_SMTP_PASSWORD is required when GITA2A_SMTP_URL is set")
	}
	address := endpoint.Host
	if endpoint.Port() == "" {
		address = net.JoinHostPort(endpoint.Hostname(), "465")
	}
	dial := d.DialTLS
	if dial == nil {
		dialer := &tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Hostname()}}
		dial = func(ctx context.Context, address string, _ *tls.Config) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", address)
		}
	}
	connection, err := dial(ctx, address, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Hostname()})
	if err != nil {
		return fmt.Errorf("email: SMTP connect: %w", err)
	}
	client, err := smtp.NewClient(connection, endpoint.Hostname())
	if err != nil {
		_ = connection.Close()
		return fmt.Errorf("email: SMTP greeting: %w", err)
	}
	defer client.Close()
	auth := smtp.PlainAuth("", user, password, endpoint.Hostname())
	if d.Auth != nil {
		auth = d.Auth(endpoint.Hostname(), user, password)
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("email: SMTP auth: %w", err)
	}
	if err := client.Mail(user); err != nil {
		return fmt.Errorf("email: SMTP sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("email: SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: SMTP data: %w", err)
	}
	if _, err := io.Copy(writer, bufio.NewReader(strings.NewReader(message))); err != nil {
		return fmt.Errorf("email: SMTP body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("email: SMTP body: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("email: SMTP quit: %w", err)
	}
	return nil
}
