APP=pidorometr3000

run:
	go run ./cmd/pidorometr3000

build:
	go build -o bin/$(APP) ./cmd/pidorometr3000

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o bin/$(APP)-linux-amd64 ./cmd/pidorometr3000

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -o bin/$(APP)-linux-arm64 ./cmd/pidorometr3000

tidy:
	go mod tidy
