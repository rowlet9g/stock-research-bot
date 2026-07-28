package emaildelivery

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSMTPSenderAuthenticatesAndSendsMessage(t *testing.T) {
	config := SMTPConfig{
		Host:     "localhost",
		Port:     465,
		Username: "smtp-user",
		Password: "smtp-password",
		TLSMode:  TLSModeImplicit,
		Timeout:  time.Second,
	}
	sender, err := NewSMTPSender(config)
	if err != nil {
		t.Fatalf("create SMTP sender: %v", err)
	}
	serverConnection, clientConnection := net.Pipe()
	defer serverConnection.Close()
	sender.dial = func(
		_ context.Context,
		_ SMTPConfig,
	) (net.Conn, error) {
		return clientConnection, nil
	}
	serverResult := make(chan fakeSMTPResult, 1)
	go runFakeSMTPServer(serverConnection, config, serverResult)

	message, err := NewMessage(
		"sender@example.test",
		[]string{"receiver@example.test"},
		"[ForgetMeNot] 일간 점검",
		"확인할 사실입니다.",
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := sender.Send(context.Background(), message); err != nil {
		t.Fatalf("send SMTP message: %v", err)
	}
	result := <-serverResult
	if result.Err != nil {
		t.Fatalf("fake SMTP server: %v", result.Err)
	}
	if result.Username != config.Username ||
		result.Password != config.Password ||
		result.MailFrom != "sender@example.test" ||
		len(result.Recipients) != 1 ||
		result.Recipients[0] != "receiver@example.test" ||
		!strings.Contains(result.Message, "Content-Type: text/plain") {
		t.Fatalf("unexpected SMTP exchange: %#v", result)
	}
}

type fakeSMTPResult struct {
	Username   string
	Password   string
	MailFrom   string
	Recipients []string
	Message    string
	Err        error
}

func runFakeSMTPServer(
	connection net.Conn,
	config SMTPConfig,
	resultChannel chan<- fakeSMTPResult,
) {
	result := fakeSMTPResult{Recipients: []string{}}
	defer func() {
		resultChannel <- result
	}()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	writeSMTPLine := func(value string) error {
		if _, err := fmt.Fprintf(writer, "%s\r\n", value); err != nil {
			return err
		}
		return writer.Flush()
	}
	readSMTPLine := func() (string, error) {
		value, err := reader.ReadString('\n')
		return strings.TrimSpace(value), err
	}
	fail := func(err error) {
		result.Err = err
	}
	if err := writeSMTPLine("220 localhost ESMTP test"); err != nil {
		fail(err)
		return
	}
	line, err := readSMTPLine()
	if err != nil || !strings.HasPrefix(line, "EHLO ") {
		fail(fmt.Errorf("expected EHLO, got %q: %w", line, err))
		return
	}
	if _, err := fmt.Fprint(
		writer,
		"250-localhost\r\n250-AUTH PLAIN\r\n250 8BITMIME\r\n",
	); err != nil {
		fail(err)
		return
	}
	if err := writer.Flush(); err != nil {
		fail(err)
		return
	}
	line, err = readSMTPLine()
	if err != nil || !strings.HasPrefix(line, "AUTH PLAIN ") {
		fail(fmt.Errorf("expected AUTH PLAIN, got %q: %w", line, err))
		return
	}
	encoded := strings.TrimPrefix(line, "AUTH PLAIN ")
	credentials, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		fail(fmt.Errorf("decode AUTH PLAIN: %w", err))
		return
	}
	parts := strings.Split(string(credentials), "\x00")
	if len(parts) != 3 {
		fail(fmt.Errorf("unexpected AUTH PLAIN payload"))
		return
	}
	result.Username = parts[1]
	result.Password = parts[2]
	if result.Username != config.Username ||
		result.Password != config.Password {
		writeSMTPLine("535 authentication failed")
		fail(fmt.Errorf("unexpected SMTP credentials"))
		return
	}
	if err := writeSMTPLine("235 authentication successful"); err != nil {
		fail(err)
		return
	}
	line, err = readSMTPLine()
	if err != nil || !strings.HasPrefix(line, "MAIL FROM:<") {
		fail(fmt.Errorf("expected MAIL FROM, got %q: %w", line, err))
		return
	}
	result.MailFrom = extractSMTPAddress(line)
	if err := writeSMTPLine("250 sender accepted"); err != nil {
		fail(err)
		return
	}
	line, err = readSMTPLine()
	if err != nil || !strings.HasPrefix(line, "RCPT TO:<") {
		fail(fmt.Errorf("expected RCPT TO, got %q: %w", line, err))
		return
	}
	result.Recipients = append(result.Recipients, extractSMTPAddress(line))
	if err := writeSMTPLine("250 recipient accepted"); err != nil {
		fail(err)
		return
	}
	line, err = readSMTPLine()
	if err != nil || line != "DATA" {
		fail(fmt.Errorf("expected DATA, got %q: %w", line, err))
		return
	}
	if err := writeSMTPLine("354 end with period"); err != nil {
		fail(err)
		return
	}
	var message strings.Builder
	for {
		line, err = reader.ReadString('\n')
		if err != nil {
			fail(err)
			return
		}
		if strings.TrimRight(line, "\r\n") == "." {
			break
		}
		message.WriteString(line)
	}
	result.Message = message.String()
	if err := writeSMTPLine("250 message accepted"); err != nil {
		fail(err)
		return
	}
	line, err = readSMTPLine()
	if err != nil || line != "QUIT" {
		fail(fmt.Errorf("expected QUIT, got %q: %w", line, err))
		return
	}
	if err := writeSMTPLine("221 goodbye"); err != nil {
		fail(err)
	}
}

func extractSMTPAddress(command string) string {
	start := strings.Index(command, "<")
	end := strings.Index(command, ">")
	if start == -1 || end <= start {
		return ""
	}
	return command[start+1 : end]
}
