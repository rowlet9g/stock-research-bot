package config

import (
	"bufio"
	"os"
	"strings"
)

type Settings struct {
	OpenDARTAPIKey string
	KRXAPIKey      string
	OpenAIAPIKey   string
	OpenAIModel    string
}

func Load(envPath string) Settings {
	loadDotEnv(envPath)
	return Settings{
		OpenDARTAPIKey: os.Getenv("OPENDART_API_KEY"),
		KRXAPIKey:      os.Getenv("KRX_API_KEY"),
		OpenAIAPIKey:   os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:    os.Getenv("OPENAI_MODEL"),
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
