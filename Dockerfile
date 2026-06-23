FROM golang:1.24-bookworm AS builder

WORKDIR /usr/src/app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -tags netgo -ldflags="-s -w" -o /run-app ./cmd/pidorometr3000

FROM debian:bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /run-app /run-app

CMD ["/run-app"]