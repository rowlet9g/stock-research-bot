package emaildelivery

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"time"
)

type smtpDialFunc func(
	ctx context.Context,
	config SMTPConfig,
) (net.Conn, error)

type SMTPSender struct {
	config SMTPConfig
	dial   smtpDialFunc
}

func NewSMTPSender(config SMTPConfig) (*SMTPSender, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &SMTPSender{
		config: config,
		dial:   dialSMTP,
	}, nil
}

func (s *SMTPSender) Send(
	ctx context.Context,
	message Message,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	connection, err := s.dial(ctx, s.config)
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer connection.Close()
	if err := setConnectionDeadline(
		connection,
		ctx,
		s.config.Timeout,
	); err != nil {
		return fmt.Errorf("set SMTP deadline: %w", err)
	}
	client, err := smtp.NewClient(connection, s.config.Host)
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()

	if s.config.TLSMode == TLSModeSTARTTLS {
		tlsConfig := secureTLSConfig(s.config.Host)
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if s.config.Username != "" {
		auth := smtp.PlainAuth(
			"",
			s.config.Username,
			s.config.Password,
			s.config.Host,
		)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("authenticate SMTP client: %w", err)
		}
	}
	if err := client.Mail(message.EnvelopeFrom()); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	for _, recipient := range message.EnvelopeRecipients() {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("set SMTP recipient: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("start SMTP message data: %w", err)
	}
	content, err := message.Bytes()
	if err != nil {
		return err
	}
	if _, err := writer.Write(content); err != nil {
		writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("finish SMTP session: %w", err)
	}
	return nil
}

func dialSMTP(
	ctx context.Context,
	config SMTPConfig,
) (net.Conn, error) {
	address := net.JoinHostPort(
		config.Host,
		strconv.Itoa(config.Port),
	)
	dialer := &net.Dialer{Timeout: config.Timeout}
	if config.TLSMode == TLSModeImplicit {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    secureTLSConfig(config.Host),
		}
		return tlsDialer.DialContext(ctx, "tcp", address)
	}
	return dialer.DialContext(ctx, "tcp", address)
}

func secureTLSConfig(host string) *tls.Config {
	return &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}
}

func setConnectionDeadline(
	connection net.Conn,
	ctx context.Context,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok &&
		contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	return connection.SetDeadline(deadline)
}
