FROM golang:1.24-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /run-app ./cmd/pidorometr3000

FROM debian:bookworm

RUN apt-get update && apt-get install -y ca-certificates && update-ca-certificates

COPY --from=builder /run-app /run-app

CMD ["/run-app"]