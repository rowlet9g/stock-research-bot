package emaildelivery

import (
	"strings"
	"testing"
)

func TestParseConfigBuildsValidatedDeliveryConfig(t *testing.T) {
	config, err := ParseConfig(RawConfig{
		SMTPHost:       "smtp.example.test",
		SMTPPort:       "587",
		SMTPUsername:   "sender@example.test",
		SMTPPassword:   "secret",
		SMTPTLSMode:    "STARTTLS",
		EmailFrom:      "ForgetMeNot <sender@example.test>",
		EmailTo:        "one@example.test, two@example.test",
		SubjectPrefix:  "",
		ReportTimeZone: "",
	})
	if err != nil {
		t.Fatalf("parse delivery config: %v", err)
	}
	if config.SMTP.Port != 587 ||
		config.SMTP.TLSMode != TLSModeSTARTTLS ||
		config.SubjectPrefix != "[ForgetMeNot]" ||
		config.ReportTimeZone != "Asia/Seoul" ||
		len(config.To) != 2 {
		t.Fatalf("unexpected delivery config: %#v", config)
	}
}

func TestParseConfigRejectsUnsafeOrIncompleteValues(t *testing.T) {
	base := RawConfig{
		SMTPHost:       "smtp.example.test",
		SMTPPort:       "587",
		SMTPUsername:   "sender@example.test",
		SMTPPassword:   "secret",
		SMTPTLSMode:    "starttls",
		EmailFrom:      "sender@example.test",
		EmailTo:        "receiver@example.test",
		ReportTimeZone: "Asia/Seoul",
	}
	tests := map[string]struct {
		mutate func(*RawConfig)
		want   string
	}{
		"missing host": {
			mutate: func(value *RawConfig) { value.SMTPHost = "" },
			want:   "SMTP_HOST",
		},
		"invalid port": {
			mutate: func(value *RawConfig) { value.SMTPPort = "0" },
			want:   "SMTP_PORT",
		},
		"unencrypted mode": {
			mutate: func(value *RawConfig) { value.SMTPTLSMode = "plain" },
			want:   "SMTP_TLS_MODE",
		},
		"password without user": {
			mutate: func(value *RawConfig) { value.SMTPUsername = "" },
			want:   "configured together",
		},
		"header injection": {
			mutate: func(value *RawConfig) {
				value.SubjectPrefix = "safe\r\nBcc: attacker@example.test"
			},
			want: "line break",
		},
		"duplicate recipient": {
			mutate: func(value *RawConfig) {
				value.EmailTo = "same@example.test,SAME@example.test"
			},
			want: "duplicate",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			_, err := ParseConfig(value)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected config error: %v", err)
			}
		})
	}
}
