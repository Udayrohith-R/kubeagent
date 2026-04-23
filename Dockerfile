FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o kubeagent ./cmd/agent

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/kubeagent /kubeagent
ENTRYPOINT ["/kubeagent"]
