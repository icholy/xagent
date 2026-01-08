.PHONY: all build clean test

all: build

build:
	@echo "Building webhooks Lambda function..."
	cd .. && \
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags lambda.norpc -o lambda/bootstrap ./cmd/lambda && \
	echo "Webhooks Lambda built successfully"

clean:
	@echo "Cleaning build artifacts..."
	rm -f bootstrap
	rm -f function.zip
	@echo "Clean complete"

test:
	@echo "Running tests..."
	cd .. && go test -v ./cmd/lambda/...
