.PHONY: build test format format-check vet lint

build:
	go build ./...

test:
	go test ./...

format:
	gofmt -w .

format-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

vet:
	go vet ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run
