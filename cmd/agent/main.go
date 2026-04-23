package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Udayrohith-R/kubeagent/internal/agent"
	"github.com/Udayrohith-R/kubeagent/internal/prometheus"
	"github.com/Udayrohith-R/kubeagent/internal/slack"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := agent.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slackClient := slack.NewClient(cfg.SlackBotToken, cfg.SlackChannelID)
	receiver := prometheus.NewWebhookReceiver(cfg.WebhookPort)
	a := agent.New(cfg, slackClient, receiver, logger)

	slog.Info("kubeagent starting", "port", cfg.WebhookPort)
	if err := a.Run(ctx); err != nil {
		slog.Error("agent exited with error", "error", err)
		os.Exit(1)
	}
}
