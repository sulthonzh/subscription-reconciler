.PHONY: build test lint run docker clean

build:
	go build -o bin/server ./cmd/server

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

run:
	go run ./cmd/server

docker:
	docker compose up --build

clean:
	rm -rf bin/ *.db
