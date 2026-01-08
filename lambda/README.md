# Lambda Functions for xagent Webhooks

This directory contains AWS Lambda functions for processing GitHub and Jira webhooks.

## Directory Structure

```
lambda/
├── github/          # GitHub webhook handler
│   ├── main.go
│   └── go.mod
├── jira/            # Jira webhook handler
│   ├── main.go
│   └── go.mod
└── README.md        # This file
```

## Building

Each Lambda function must be built for Linux AMD64 or ARM64 architecture:

### GitHub Lambda

```bash
cd github
GOOS=linux GOARCH=amd64 go build -tags lambda.norpc -o bootstrap main.go
zip function.zip bootstrap
cd ..
```

### Jira Lambda

```bash
cd jira
GOOS=linux GOARCH=amd64 go build -tags lambda.norpc -o bootstrap main.go
zip function.zip bootstrap
cd ..
```

**Note:** The binary must be named `bootstrap` for the `provided.al2023` runtime.

## Testing Locally

You can test Lambda functions locally using AWS SAM or the Lambda runtime emulator.

### Using AWS SAM

Install AWS SAM CLI, then:

```bash
# Test GitHub Lambda
cd github
sam local invoke -e test-event.json

# Test Jira Lambda
cd jira
sam local invoke -e test-event.json
```

### Example Test Events

**GitHub test event (test-event.json):**

```json
{
  "headers": {
    "x-github-event": "issue_comment",
    "x-hub-signature-256": "sha256=<calculated-signature>"
  },
  "body": "{\"action\":\"created\",\"issue\":{\"number\":1,\"html_url\":\"https://github.com/owner/repo/issues/1\"},\"comment\":{\"body\":\"xagent new Test task\",\"user\":{\"login\":\"testuser\"}},\"repository\":{\"full_name\":\"owner/repo\"}}"
}
```

**Jira test event (test-event.json):**

```json
{
  "headers": {
    "x-hub-signature": "<calculated-signature>"
  },
  "body": "{\"webhookEvent\":\"comment_created\",\"issue\":{\"key\":\"PROJ-123\"},\"comment\":{\"body\":\"xagent new Test task\"}}"
}
```

## Environment Variables

### GitHub Lambda

- `SQS_QUEUE_URL` (required): URL of the SQS queue
- `GITHUB_WEBHOOK_SECRET` (required): Secret for signature verification

### Jira Lambda

- `SQS_QUEUE_URL` (required): URL of the SQS queue
- `JIRA_WEBHOOK_SECRET` (required): Secret for signature verification
- `JIRA_BASE_URL` (required): Base URL of Jira instance (e.g., https://your-domain.atlassian.net)

## Webhook Signature Verification

### GitHub

GitHub signs webhook payloads using HMAC-SHA256:

```
X-Hub-Signature-256: sha256=<signature>
```

The signature is calculated as:

```
HMAC-SHA256(payload, secret)
```

### Jira

Jira signs webhook payloads using HMAC-SHA256 with Base64 encoding:

```
X-Hub-Signature: <base64-encoded-signature>
```

The signature is calculated as:

```
Base64(HMAC-SHA256(payload, secret))
```

## Event Processing

Both Lambda functions:

1. Verify webhook signature
2. Parse webhook payload
3. Extract relevant information (comment body, issue/PR URL)
4. Filter for comments starting with "xagent task" or "xagent new"
5. Transform to xagent event format
6. Publish to SQS queue

### XAgent Event Format

```json
{
  "description": "xagent new Fix the bug",
  "data": "<raw webhook payload>",
  "url": "https://github.com/owner/repo/pull/123"
}
```

- `description`: The comment text (used as task instruction)
- `data`: Complete webhook payload (for debugging and advanced processing)
- `url`: URL to the GitHub PR/issue or Jira issue

## Deployment

The Lambda functions are deployed using Terraform. See `../terraform/` for infrastructure code.

Manual deployment:

```bash
# Deploy GitHub Lambda
aws lambda update-function-code \
  --function-name xagent-github-webhook \
  --zip-file fileb://github/function.zip

# Deploy Jira Lambda
aws lambda update-function-code \
  --function-name xagent-jira-webhook \
  --zip-file fileb://jira/function.zip
```

## Monitoring

Lambda function logs are available in CloudWatch Logs:

- GitHub Lambda: `/aws/lambda/xagent-github-webhook`
- Jira Lambda: `/aws/lambda/xagent-jira-webhook`

## Dependencies

Dependencies are managed via `go.mod` in each function directory:

- `github.com/aws/aws-lambda-go` - Lambda runtime
- `github.com/aws/aws-sdk-go-v2` - AWS SDK for SQS

## Error Handling

Lambda functions return appropriate HTTP status codes:

- `200 OK`: Event processed successfully or ignored
- `400 Bad Request`: Invalid JSON payload
- `401 Unauthorized`: Invalid signature
- `500 Internal Server Error`: Failed to publish to SQS

Failed events can be retried by the webhook provider (GitHub/Jira) or will appear in CloudWatch logs for debugging.
