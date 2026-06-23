FROM golang:1.24-bookworm AS builder

WORKDIR /usr/src/app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN go build -tags netgo -ldflags="-s -w" -o /run-app ./cmd/pidorometr3000

FROM debian:bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /run-app /run-app

CMD ["/run-app"]