package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHost           = "127.0.0.1"
	defaultPort           = "8080"
	defaultOpenAIBaseURL  = "https://api.openai.com/v1"
	defaultModel          = "gpt-4o-mini"
	defaultRequestTimeout = 5 * time.Minute
)

// Config contains runtime settings for the Go backend.
type Config struct {
	Server  ServerConfig
	LLM     LLMConfig
	DataDir string
}

type ServerConfig struct {
	Host string
	Port string
}

type LLMConfig struct {
	APIKey         string
	BaseURL        string
	Model          string
	RequestTimeout time.Duration
}

// if env file loads then cfg := Config(env) then validate config, return config
func Load(envFile string) (Config, error) {
	if envFile != "" {
		if err := LoadDotEnv(envFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
	}

	cfg := Config{
		Server: ServerConfig{
			Host: getenv("HOST", defaultHost),
			Port: getenv("PORT", defaultPort),
		},
		LLM: LLMConfig{
			APIKey:         firstEnv("OPENAI_API_KEY", "LLM_API_KEY"),
			BaseURL:        strings.TrimRight(getenvAny([]string{"OPENAI_BASE_URL", "LLM_BASE_URL"}, defaultOpenAIBaseURL), "/"),
			Model:          getenvAny([]string{"OPENAI_MODEL", "MODEL_NAME", "LLM_MODEL"}, defaultModel),
			RequestTimeout: durationFromEnv("LLM_REQUEST_TIMEOUT", defaultRequestTimeout),
		},
		DataDir: getenv("DATA_DIR", "data"),
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Server.Port) == "" {
		return errors.New("PORT must not be empty")
	}
	if _, err := strconv.Atoi(c.Server.Port); err != nil { //Atoi is essentially a .ToString()
		return fmt.Errorf("PORT must be numeric: %w", err)
	}
	if strings.TrimSpace(c.LLM.APIKey) == "" {
		return errors.New("OPENAI_API_KEY or LLM_API_KEY is required")
	}
	if strings.TrimSpace(c.LLM.Model) == "" {
		return errors.New("OPENAI_MODEL, MODEL_NAME, or LLM_MODEL is required")
	}
	if _, err := url.ParseRequestURI(c.LLM.BaseURL); err != nil {
		return fmt.Errorf("OPENAI_BASE_URL or LLM_BASE_URL must be a valid URL: %w", err)
	}
	if c.LLM.RequestTimeout <= 0 {
		return errors.New("LLM_REQUEST_TIMEOUT must be positive")
	}
	return nil
}

func (c Config) Addr() string {
	return c.Server.Host + ":" + c.Server.Port
}

func (c Config) ChatCompletionsURL() string {
	return strings.TrimRight(c.LLM.BaseURL, "/") + "/chat/completions"
}

// LoadDotEnv loads a simple KEY=VALUE env file. It intentionally supports only
// the common dotenv subset needed for local development.
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") { //commented out lines ignored
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)

		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func getenv(key, fallback string) string { //overloaded os.Getenv with fallback
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// if value exists in the enviroment provided it is not empty return it otherwise fallback
func getenvAny(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func firstEnv(keys ...string) string { //zero or more string args
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	duration, err := time.ParseDuration(raw)
	if err == nil {
		return duration
	}

	seconds, err := strconv.Atoi(raw) //strings and strconv are golang's, see imports
	if err != nil {
		return fallback
	}

	return time.Duration(seconds) * time.Second
}
