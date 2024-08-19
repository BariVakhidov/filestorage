CC=go

lint:
	@golangci-lint run -c ./golangci.yml ./...

build: lint
	@$(CC) build -o bin/app

run: build
	@./bin/app

test:
	@$(CC) test -v ./...

