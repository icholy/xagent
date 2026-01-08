# Webhook-Based Event Architecture Setup Guide

This guide explains how to set up the webhook-based architecture for xagent using AWS Lambda and SQS to replace the polling-based approach.

## Architecture Overview

```
GitHub/Jira Webhook → Lambda Function URL → Transform → SQS → xagent subscribe → xagent server
```

### Components

1. **AWS Lambda with Function URLs**
   - Two separate Lambda functions for GitHub and Jira webhooks
   - Transform webhook payloads into xagent event structure
   - Publish events to SQS queue

2. **Amazon SQS Queue**
   - Acts as a message buffer between Lambda and xagent
   - Provides durability and retry mechanisms
   - Dead-letter queue for failed events

3. **xagent subscribe Command**
   - Polls SQS for events
   - Processes events and creates/updates tasks via xagent server API
   - Runs as a separate process alongside xagent server

## Prerequisites

- AWS account with appropriate permissions
- Terraform >= 1.0 installed
- Go 1.23 or later
- Access to GitHub/Jira with admin permissions to configure webhooks

## Infrastructure Setup

### 1. Build Lambda Functions

First, build the Lambda functions for deployment:

```bash
# Build GitHub Lambda
cd lambda/github
GOOS=linux GOARCH=amd64 go build -tags lambda.norpc -o bootstrap main.go
cd ../..

# Build Jira Lambda
cd lambda/jira
GOOS=linux GOARCH=amd64 go build -tags lambda.norpc -o bootstrap main.go
cd ../..
```

### 2. Configure Terraform Variables

Copy the example terraform variables file and edit it:

```bash
cd terraform
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` with your values:

```hcl
aws_region   = "us-east-1"
project_name = "xagent"

# Generate secrets using: openssl rand -hex 32
github_webhook_secret = "your-github-webhook-secret-here"
jira_webhook_secret   = "your-jira-webhook-secret-here"
jira_base_url         = "https://your-domain.atlassian.net"
```

**Important:** Keep your webhook secrets secure! These are used to verify that webhooks are coming from legitimate sources.

### 3. Deploy Infrastructure

Initialize and apply Terraform:

```bash
terraform init
terraform plan
terraform apply
```

After successful deployment, note the output values:

- `github_webhook_url` - Configure this in GitHub
- `jira_webhook_url` - Configure this in Jira
- `sqs_queue_url` - Use this with `xagent subscribe`

## Webhook Configuration

### GitHub Webhook Setup

1. Go to your GitHub repository settings
2. Navigate to **Settings** → **Webhooks** → **Add webhook**
3. Configure the webhook:
   - **Payload URL**: Use the `github_webhook_url` from Terraform output
   - **Content type**: `application/json`
   - **Secret**: Use the same `github_webhook_secret` from terraform.tfvars
   - **Events**: Select individual events:
     - Issue comments
     - Pull request review comments
     - Pull requests
4. Click **Add webhook**

### Jira Webhook Setup

1. Go to your Jira instance
2. Navigate to **Settings** → **System** → **WebHooks**
3. Click **Create a WebHook**
4. Configure the webhook:
   - **Name**: xagent webhook
   - **Status**: Enabled
   - **URL**: Use the `jira_webhook_url` from Terraform output
   - **Events**: Select:
     - Issue → commented
5. Click **Create**

**Note:** Jira Cloud webhooks don't support signature verification in the same way as GitHub. The Lambda function includes basic validation, but you should ensure your Jira instance is properly secured.

## Running xagent subscribe

Start the subscribe command to process events from SQS:

```bash
export SQS_QUEUE_URL="<queue-url-from-terraform-output>"
export AWS_REGION="us-east-1"

xagent subscribe \
  --queue-url "$SQS_QUEUE_URL" \
  --workspace <your-workspace> \
  --server http://localhost:8080
```

Or using environment variables:

```bash
export SQS_QUEUE_URL="<queue-url-from-terraform-output>"
export AWS_REGION="us-east-1"

xagent subscribe -w <your-workspace>
```

### Command Options

- `--queue-url, -q`: SQS queue URL (or env: `SQS_QUEUE_URL`)
- `--workspace, -ws`: Workspace for new tasks (required)
- `--server, -s`: xagent server URL (default: `http://localhost:8080`)
- `--max-messages, -m`: Max messages per poll (default: 10)
- `--wait-time, -w`: Long polling wait time in seconds (default: 20)
- `--poll-interval`: Interval between polls when queue is empty (default: 5s)
- `--region, -r`: AWS region (or env: `AWS_REGION`)

## Usage

### Creating New Tasks

Comment on a GitHub PR or Jira issue with:

```
xagent new <your instructions here>
```

This will create a new task in the specified workspace and link it to the PR/issue.

### Notifying Existing Tasks

Comment on a linked GitHub PR or Jira issue with:

```
xagent task <additional instructions or updates>
```

This will create an event that notifies all tasks linked to that PR/issue.

## Event Flow Details

### GitHub Events

The GitHub Lambda function processes the following webhook events:

- `issue_comment` - Comments on issues
- `pull_request_review_comment` - Review comments on PRs
- `pull_request` - PR events (with comments)

Only comments starting with `xagent task` or `xagent new` are processed.

### Jira Events

The Jira Lambda function processes:

- `comment_created` - New comments on issues

Only comments starting with `xagent task` or `xagent new` are processed.

### Event Structure

Events published to SQS have this structure:

```json
{
  "description": "xagent new Fix the authentication bug",
  "data": "<raw webhook payload>",
  "url": "https://github.com/owner/repo/pull/123"
}
```

The `data` field contains the complete webhook payload for debugging and advanced processing.

## Monitoring and Debugging

### CloudWatch Logs

Lambda function logs are available in CloudWatch:

```bash
# View GitHub Lambda logs
aws logs tail /aws/lambda/xagent-github-webhook --follow

# View Jira Lambda logs
aws logs tail /aws/lambda/xagent-jira-webhook --follow
```

### SQS Queue Monitoring

Check the SQS queue for pending messages:

```bash
aws sqs get-queue-attributes \
  --queue-url "<your-queue-url>" \
  --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible
```

### Dead Letter Queue

Failed messages are sent to the dead-letter queue after 3 retry attempts:

```bash
# View messages in DLQ
aws sqs receive-message \
  --queue-url "<your-dlq-url>" \
  --max-number-of-messages 10
```

## Troubleshooting

### Lambda Function Errors

1. **Authentication errors**: Verify webhook secrets match between Terraform and webhook configuration
2. **SQS permission errors**: Check IAM role has `sqs:SendMessage` permission
3. **Timeout errors**: Increase Lambda timeout in Terraform (default: 30s)

### Subscribe Command Issues

1. **No messages received**: Verify events are being sent to SQS via CloudWatch logs
2. **Authentication errors**: Ensure AWS credentials are configured (`aws configure`)
3. **Task creation errors**: Check xagent server is running and accessible

### Webhook Delivery Failures

**GitHub:**
- Check webhook delivery history in GitHub settings
- Look for signature verification failures

**Jira:**
- Check webhook configuration in Jira settings
- Verify the Lambda URL is accessible from Jira Cloud

## Migration from Polling

To migrate from the polling-based approach (`xagent jira` and `xagent github`):

1. Deploy the webhook infrastructure using Terraform
2. Configure webhooks in GitHub/Jira
3. Start the `xagent subscribe` command
4. Verify webhooks are working correctly
5. Stop the `xagent jira` and `xagent github` polling commands
6. (Optional) Remove polling-related configuration

## Cost Estimation

AWS costs for this architecture are typically very low:

- **Lambda**: Free tier includes 1M requests/month and 400,000 GB-seconds of compute
- **SQS**: Free tier includes 1M requests/month
- **Data transfer**: Minimal for webhook payloads

For a typical small to medium usage (< 1000 webhooks/day), the cost should remain within the AWS free tier.

## Security Considerations

1. **Webhook Secrets**: Always use strong, randomly-generated secrets
2. **Function URLs**: Currently set to public (NONE authorization) but validated via signatures
3. **SQS Access**: Lambda functions have minimal IAM permissions (SendMessage only)
4. **Credentials**: Never commit `terraform.tfvars` to version control
5. **AWS Credentials**: The `xagent subscribe` command requires AWS credentials with SQS read/delete permissions

## Next Steps

- Set up CloudWatch alarms for Lambda errors and DLQ messages
- Configure auto-scaling for high-volume scenarios
- Implement custom event filtering based on labels or other criteria
- Add support for additional webhook sources
