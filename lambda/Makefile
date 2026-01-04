.PHONY: all build clean test

all: build

build:
	@echo "Building webhooks Lambda function..."
	cd webhooks && \
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags lambda.norpc -o bootstrap main.go && \
	echo "Webhooks Lambda built successfully"

clean:
	@echo "Cleaning build artifacts..."
	rm -f webhooks/bootstrap
	rm -f webhooks/function.zip
	@echo "Clean complete"

test:
	@echo "Running tests..."
	cd webhooks && go test -v ./...
