NAME ?= go-tf-provisioner

.PHONY: build clean test test-verbose coverage mockgen lint help

build: ## Build the binary
	go build -ldflags='-s -w' -o bin/$(NAME) .

docker-build: ## Build the Docker image
	docker build -t $(NAME):latest .

lint: ## Run linter
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run

mockgen: ## Generate mocks
	go generate ./...

test: ## Run tests with coverage
	go test ./... -coverprofile=coverage.out
	go tool gocover-cobertura < coverage.out > coverage.xml
