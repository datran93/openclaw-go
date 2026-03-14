.PHONY: all build run test clean

APP_NAME=openclaw

all: build

build:
	go build -o bin/$(APP_NAME) cmd/openclaw/main.go

run:
	go run cmd/openclaw/main.go

test:
	go test -v -cover ./...

clean:
	rm -rf bin/
