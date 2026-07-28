package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsEmailSettingsWithoutOverridingEnvironment(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	content := []byte(
		"SMTP_HOST=smtp.example.test\n" +
			"SMTP_PORT=587\n" +
			"SMTP_USERNAME=dotenv-user\n" +
			"SMTP_PASSWORD=dotenv-password\n" +
			"SMTP_TLS_MODE=starttls\n" +
			"EMAIL_FROM=from@example.test\n" +
			"EMAIL_TO=one@example.test,two@example.test\n" +
			"EMAIL_SUBJECT_PREFIX=[Test]\n" +
			"EMAIL_REPORT_TIMEZONE=Asia/Seoul\n",
	)
	if err := os.WriteFile(envPath, content, 0o600); err != nil {
		t.Fatalf("write test environment: %v", err)
	}
	t.Setenv("SMTP_USERNAME", "environment-user")
	for _, key := range []string{
		"SMTP_HOST",
		"SMTP_PORT",
		"SMTP_PASSWORD",
		"SMTP_TLS_MODE",
		"EMAIL_FROM",
		"EMAIL_TO",
		"EMAIL_SUBJECT_PREFIX",
		"EMAIL_REPORT_TIMEZONE",
	} {
		t.Setenv(key, "")
	}

	settings := Load(envPath)
	if settings.SMTPHost != "smtp.example.test" ||
		settings.SMTPPort != "587" ||
		settings.SMTPUsername != "environment-user" ||
		settings.SMTPPassword != "dotenv-password" ||
		settings.SMTPTLSMode != "starttls" ||
		settings.EmailFrom != "from@example.test" ||
		settings.EmailTo != "one@example.test,two@example.test" ||
		settings.EmailSubjectPrefix != "[Test]" ||
		settings.EmailReportTimeZone != "Asia/Seoul" {
		t.Fatalf("unexpected email settings: %#v", settings)
	}
}
