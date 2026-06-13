FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /api ./cmd/api

# ─── Final image ───────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12

COPY --from=builder /api /api

# Run as non-root user (distroless nonroot user is 65532)
USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/api"]
