package config

import (
	"bufio"
	"os"
	"strings"
)

type Settings struct {
	OpenDARTAPIKey      string
	KRXAPIKey           string
	OpenAIAPIKey        string
	OpenAIModel         string
	SMTPHost            string
	SMTPPort            string
	SMTPUsername        string
	SMTPPassword        string
	SMTPTLSMode         string
	EmailFrom           string
	EmailTo             string
	EmailSubjectPrefix  string
	EmailReportTimeZone string
}

func Load(envPath string) Settings {
	loadDotEnv(envPath)
	return Settings{
		OpenDARTAPIKey:      os.Getenv("OPENDART_API_KEY"),
		KRXAPIKey:           os.Getenv("KRX_API_KEY"),
		OpenAIAPIKey:        os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:         os.Getenv("OPENAI_MODEL"),
		SMTPHost:            os.Getenv("SMTP_HOST"),
		SMTPPort:            os.Getenv("SMTP_PORT"),
		SMTPUsername:        os.Getenv("SMTP_USERNAME"),
		SMTPPassword:        os.Getenv("SMTP_PASSWORD"),
		SMTPTLSMode:         os.Getenv("SMTP_TLS_MODE"),
		EmailFrom:           os.Getenv("EMAIL_FROM"),
		EmailTo:             os.Getenv("EMAIL_TO"),
		EmailSubjectPrefix:  os.Getenv("EMAIL_SUBJECT_PREFIX"),
		EmailReportTimeZone: os.Getenv("EMAIL_REPORT_TIMEZONE"),
	}
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" && os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
