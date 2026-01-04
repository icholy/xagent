.PHONY: all build-github build-jira clean test

all: build-github build-jira

build-github:
	@echo "Building GitHub Lambda function..."
	cd github && \
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags lambda.norpc -o bootstrap main.go && \
	echo "GitHub Lambda built successfully"

build-jira:
	@echo "Building Jira Lambda function..."
	cd jira && \
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags lambda.norpc -o bootstrap main.go && \
	echo "Jira Lambda built successfully"

clean:
	@echo "Cleaning build artifacts..."
	rm -f github/bootstrap jira/bootstrap
	rm -f github/function.zip jira/function.zip
	@echo "Clean complete"

test:
	@echo "Running tests..."
	cd github && go test -v ./...
	cd jira && go test -v ./...
