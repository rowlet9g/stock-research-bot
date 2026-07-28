package emaildelivery

import (
	"bytes"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

type Message struct {
	from    *mail.Address
	to      []*mail.Address
	subject string
	body    string
	date    time.Time
}

func NewMessage(
	from string,
	to []string,
	subject string,
	body string,
	date time.Time,
) (Message, error) {
	fromAddress, err := parseMailbox(from)
	if err != nil {
		return Message{}, fmt.Errorf("parse sender address: %w", err)
	}
	if len(to) == 0 {
		return Message{}, fmt.Errorf(
			"email message requires at least one recipient",
		)
	}
	recipients := make([]*mail.Address, 0, len(to))
	for _, value := range to {
		address, err := parseMailbox(value)
		if err != nil {
			return Message{}, fmt.Errorf(
				"parse recipient address: %w",
				err,
			)
		}
		recipients = append(recipients, address)
	}
	subject = strings.TrimSpace(subject)
	switch {
	case subject == "":
		return Message{}, fmt.Errorf("email subject is required")
	case containsHeaderBreak(subject):
		return Message{}, fmt.Errorf("email subject contains a line break")
	case strings.TrimSpace(body) == "":
		return Message{}, fmt.Errorf("email body is required")
	case date.IsZero():
		return Message{}, fmt.Errorf("email date is required")
	}
	return Message{
		from:    fromAddress,
		to:      recipients,
		subject: subject,
		body:    body,
		date:    date.UTC(),
	}, nil
}

func (m Message) EnvelopeFrom() string {
	return m.from.Address
}

func (m Message) EnvelopeRecipients() []string {
	recipients := make([]string, 0, len(m.to))
	for _, address := range m.to {
		recipients = append(recipients, address.Address)
	}
	return recipients
}

func (m Message) Bytes() ([]byte, error) {
	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, "Date: %s\r\n", m.date.Format(time.RFC1123Z))
	fmt.Fprintf(&buffer, "From: %s\r\n", m.from.String())
	toHeaders := make([]string, 0, len(m.to))
	for _, address := range m.to {
		toHeaders = append(toHeaders, address.String())
	}
	fmt.Fprintf(&buffer, "To: %s\r\n", strings.Join(toHeaders, ", "))
	fmt.Fprintf(
		&buffer,
		"Subject: %s\r\n",
		mime.QEncoding.Encode("UTF-8", m.subject),
	)
	buffer.WriteString("MIME-Version: 1.0\r\n")
	buffer.WriteString(
		"Content-Type: text/plain; charset=UTF-8\r\n" +
			"Content-Transfer-Encoding: quoted-printable\r\n\r\n",
	)
	writer := quotedprintable.NewWriter(&buffer)
	body := strings.ReplaceAll(
		strings.ReplaceAll(m.body, "\r\n", "\n"),
		"\r",
		"\n",
	)
	body = strings.ReplaceAll(body, "\n", "\r\n")
	if _, err := writer.Write([]byte(body)); err != nil {
		return nil, fmt.Errorf("encode email body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish email body: %w", err)
	}
	return buffer.Bytes(), nil
}
