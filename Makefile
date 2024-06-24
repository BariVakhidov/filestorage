lint:
	@golangci-lint run -c ./golangci.yml ./...

build:
	@go build -o bin/app

run: lint build
	@./bin/app

test:
	@go test -v ./...

