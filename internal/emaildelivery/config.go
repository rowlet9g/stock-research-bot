package emaildelivery

import (
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

type TLSMode string

const (
	TLSModeSTARTTLS TLSMode = "starttls"
	TLSModeImplicit TLSMode = "implicit"
)

type SMTPConfig struct {
	Host     string        `json:"host"`
	Port     int           `json:"port"`
	Username string        `json:"username,omitempty"`
	Password string        `json:"-"`
	TLSMode  TLSMode       `json:"tls_mode"`
	Timeout  time.Duration `json:"timeout"`
}

type DeliveryConfig struct {
	SMTP           SMTPConfig `json:"smtp"`
	From           string     `json:"from"`
	To             []string   `json:"to"`
	SubjectPrefix  string     `json:"subject_prefix"`
	ReportTimeZone string     `json:"report_time_zone"`
}

type RawConfig struct {
	SMTPHost       string
	SMTPPort       string
	SMTPUsername   string
	SMTPPassword   string
	SMTPTLSMode    string
	EmailFrom      string
	EmailTo        string
	SubjectPrefix  string
	ReportTimeZone string
}

func ParseConfig(raw RawConfig) (DeliveryConfig, error) {
	port, err := strconv.Atoi(strings.TrimSpace(raw.SMTPPort))
	if err != nil {
		return DeliveryConfig{}, fmt.Errorf(
			"SMTP_PORT must be an integer",
		)
	}
	recipients := splitRecipients(raw.EmailTo)
	config := DeliveryConfig{
		SMTP: SMTPConfig{
			Host:     strings.TrimSpace(raw.SMTPHost),
			Port:     port,
			Username: strings.TrimSpace(raw.SMTPUsername),
			Password: raw.SMTPPassword,
			TLSMode: TLSMode(
				strings.ToLower(strings.TrimSpace(raw.SMTPTLSMode)),
			),
			Timeout: 30 * time.Second,
		},
		From:           strings.TrimSpace(raw.EmailFrom),
		To:             recipients,
		SubjectPrefix:  strings.TrimSpace(raw.SubjectPrefix),
		ReportTimeZone: strings.TrimSpace(raw.ReportTimeZone),
	}
	if config.SubjectPrefix == "" {
		config.SubjectPrefix = "[ForgetMeNot]"
	}
	if config.ReportTimeZone == "" {
		config.ReportTimeZone = "Asia/Seoul"
	}
	if err := config.Validate(); err != nil {
		return DeliveryConfig{}, err
	}
	return config, nil
}

func (c DeliveryConfig) Validate() error {
	if err := c.SMTP.Validate(); err != nil {
		return err
	}
	if _, err := parseMailbox(c.From); err != nil {
		return fmt.Errorf("EMAIL_FROM is invalid: %w", err)
	}
	if len(c.To) == 0 {
		return fmt.Errorf("EMAIL_TO must contain at least one address")
	}
	seen := map[string]struct{}{}
	for _, recipient := range c.To {
		address, err := parseMailbox(recipient)
		if err != nil {
			return fmt.Errorf("EMAIL_TO contains an invalid address: %w", err)
		}
		key := strings.ToLower(address.Address)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("EMAIL_TO contains duplicate address %q", address.Address)
		}
		seen[key] = struct{}{}
	}
	if containsHeaderBreak(c.SubjectPrefix) {
		return fmt.Errorf("EMAIL_SUBJECT_PREFIX contains a line break")
	}
	if _, err := time.LoadLocation(c.ReportTimeZone); err != nil {
		return fmt.Errorf(
			"EMAIL_REPORT_TIMEZONE %q is invalid: %w",
			c.ReportTimeZone,
			err,
		)
	}
	return nil
}

func (c SMTPConfig) Validate() error {
	switch {
	case strings.TrimSpace(c.Host) == "":
		return fmt.Errorf("SMTP_HOST is required")
	case c.Port <= 0 || c.Port > 65535:
		return fmt.Errorf("SMTP_PORT must be between 1 and 65535")
	case c.TLSMode != TLSModeSTARTTLS && c.TLSMode != TLSModeImplicit:
		return fmt.Errorf(
			`SMTP_TLS_MODE must be "starttls" or "implicit"`,
		)
	case c.Timeout <= 0:
		return fmt.Errorf("SMTP timeout must be greater than zero")
	case (c.Username == "") != (c.Password == ""):
		return fmt.Errorf(
			"SMTP_USERNAME and SMTP_PASSWORD must be configured together",
		)
	}
	if strings.ContainsAny(c.Host, " \t\r\n") {
		return fmt.Errorf("SMTP_HOST contains whitespace")
	}
	return nil
}

func splitRecipients(value string) []string {
	parts := strings.Split(value, ",")
	recipients := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			recipients = append(recipients, trimmed)
		}
	}
	return recipients
}

func parseMailbox(value string) (*mail.Address, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("address is required")
	}
	address, err := mail.ParseAddress(value)
	if err != nil {
		return nil, err
	}
	if containsHeaderBreak(address.Name) ||
		containsHeaderBreak(address.Address) {
		return nil, fmt.Errorf("address contains a line break")
	}
	return address, nil
}

func containsHeaderBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}
