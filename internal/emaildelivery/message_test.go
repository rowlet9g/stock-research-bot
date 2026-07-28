package emaildelivery

import (
	"io"
	"mime/quotedprintable"
	"strings"
	"testing"
	"time"
)

func TestMessageBuildsUTF8MIMEContent(t *testing.T) {
	message, err := NewMessage(
		"ForgetMeNot <sender@example.test>",
		[]string{"receiver@example.test"},
		"[ForgetMeNot] 일간 투자 점검",
		"사실: 집중도를 확인해야 합니다.\n",
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("create email message: %v", err)
	}
	content, err := message.Bytes()
	if err != nil {
		t.Fatalf("build email message: %v", err)
	}
	text := string(content)
	for _, expected := range []string{
		"From: \"ForgetMeNot\" <sender@example.test>",
		"To: <receiver@example.test>",
		"Subject: =?UTF-8?q?",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("email content missing %q:\n%s", expected, text)
		}
	}
	parts := strings.SplitN(text, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("email MIME body missing:\n%s", text)
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(
		strings.NewReader(parts[1]),
	))
	if err != nil {
		t.Fatalf("decode email body: %v", err)
	}
	if !strings.Contains(string(decoded), "사실: 집중도를 확인해야 합니다.") {
		t.Fatalf("unexpected decoded email body: %s", decoded)
	}
}

func TestMessageRejectsHeaderInjection(t *testing.T) {
	_, err := NewMessage(
		"sender@example.test",
		[]string{"receiver@example.test"},
		"subject\r\nBcc: attacker@example.test",
		"body",
		time.Now(),
	)
	if err == nil || !strings.Contains(err.Error(), "line break") {
		t.Fatalf("unexpected subject error: %v", err)
	}
}
