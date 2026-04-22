FROM golang:1.25 AS builder

WORKDIR /app
COPY . .

RUN go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o goro ./cmd/goro

FROM debian:stable-slim

WORKDIR /app
COPY --from=builder /app/goro /app/goro

RUN apt-get update && apt-get install -y sqlite3 ffmpeg && rm -rf /var/lib/apt/lists/*

CMD ["/app/goro"]