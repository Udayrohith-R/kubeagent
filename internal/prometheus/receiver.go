package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Alert represents a single Alertmanager alert payload.
type Alert struct {
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
}

type alertmanagerPayload struct {
	Alerts []struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
	} `json:"alerts"`
}

// WebhookReceiver listens for Alertmanager webhook POST requests and
// forwards decoded alerts onto a channel for the agent to process.
type WebhookReceiver struct {
	port int
}

func NewWebhookReceiver(port int) *WebhookReceiver {
	return &WebhookReceiver{port: port}
}

func (r *WebhookReceiver) Start(ctx context.Context) (<-chan Alert, error) {
	ch := make(chan Alert, 64)

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload alertmanagerPayload
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			slog.Warn("bad webhook payload", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		for _, a := range payload.Alerts {
			if a.Status != "firing" {
				continue
			}
			alert := Alert{
				Name:        a.Labels["alertname"],
				Status:      a.Status,
				Labels:      a.Labels,
				Annotations: a.Annotations,
				StartsAt:    a.StartsAt,
			}
			select {
			case ch <- alert:
			default:
				slog.Warn("alert channel full, dropping alert", "alert", alert.Name)
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", r.port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down webhook server")
		_ = srv.Close()
		close(ch)
	}()

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("webhook server error", "error", err)
		}
	}()

	return ch, nil
}
