package agent

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	WebhookPort     int
	KubeconfigPath  string
	SlackBotToken   string
	SlackChannelID  string
	AnthropicAPIKey string
	LLMModel        string
	MaxDiagDepth    int
}

func LoadConfig() (*Config, error) {
	port, err := strconv.Atoi(getEnv("WEBHOOK_PORT", "8080"))
	if err != nil {
		return nil, fmt.Errorf("invalid WEBHOOK_PORT: %w", err)
	}

	depth, err := strconv.Atoi(getEnv("MAX_DIAG_DEPTH", "3"))
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_DIAG_DEPTH: %w", err)
	}

	slackToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackToken == "" {
		return nil, fmt.Errorf("SLACK_BOT_TOKEN is required")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required")
	}

	return &Config{
		WebhookPort:     port,
		KubeconfigPath:  getEnv("KUBECONFIG", ""),
		SlackBotToken:   slackToken,
		SlackChannelID:  getEnv("SLACK_CHANNEL_ID", "#incidents"),
		AnthropicAPIKey: apiKey,
		LLMModel:        getEnv("LLM_MODEL", "claude-3-5-sonnet-20241022"),
		MaxDiagDepth:    depth,
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
