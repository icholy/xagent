# Webhook-Based Event Architecture Setup Guide

This guide explains how to set up the webhook-based architecture for xagent using AWS Lambda to receive GitHub and Jira webhooks and forward them directly to the xagent server via RPC.

## Architecture Overview

```
GitHub/Jira Webhook → Lambda Function URL → xagent server (RPC)
```

### Components

1. **AWS Lambda with Function URLs**
   - Receives GitHub and Jira webhooks
   - Validates webhook signatures
   - Transforms webhook payloads into xagent events
   - Creates and processes events directly via xagent RPC API using an API key

## Prerequisites

- AWS account with appropriate permissions
- Terraform >= 1.0 installed
- Go 1.23 or later
- Access to GitHub/Jira with admin permissions to configure webhooks
- An xagent API key (generate one in the xagent web UI)

## Infrastructure Setup

### 1. Build the Lambda Function

Build the Lambda function for deployment:

```bash
cd cmd/webhook
GOOS=linux GOARCH=amd64 go build -tags lambda.norpc -o bootstrap .
cd ../..
```

### 2. Configure Terraform Variables

Set up your secrets in the Terraform secrets file. The Lambda requires:

- `xagent_server` - URL of the xagent server (e.g., `https://xagent.example.com`)
- `xagent_token` - An xagent API key (starts with `xat_`)
- `github_webhook_secret` - Secret for verifying GitHub webhooks
- `jira_webhook_secret` - Secret for verifying Jira webhooks
- `jira_base_url` - Base URL of your Jira instance

### 3. Deploy Infrastructure

Initialize and apply Terraform:

```bash
cd terraform
terraform init
terraform plan
terraform apply
```

After successful deployment, note the output values:

- `webhook_url` - Configure this in GitHub and Jira (with `/webhook/github` or `/webhook/jira` path)

## Webhook Configuration

### GitHub Webhook Setup

1. Go to your GitHub repository settings
2. Navigate to **Settings** → **Webhooks** → **Add webhook**
3. Configure the webhook:
   - **Payload URL**: Use the Lambda function URL with `/webhook/github` path
   - **Content type**: `application/json`
   - **Secret**: Use the same `github_webhook_secret` from your secrets
   - **Events**: Select individual events:
     - Issue comments
     - Pull request review comments
     - Pull request reviews
4. Click **Add webhook**

### Jira Webhook Setup

1. Go to your Jira instance
2. Navigate to **Settings** → **System** → **WebHooks**
3. Click **Create a WebHook**
4. Configure the webhook:
   - **Name**: xagent webhook
   - **Status**: Enabled
   - **URL**: Use the Lambda function URL with `/webhook/jira` path
   - **Events**: Select:
     - Issue → commented
5. Click **Create**

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

The Lambda function processes the following webhook events:

- `issue_comment` - Comments on issues and PRs
- `pull_request_review_comment` - Review comments on PRs
- `pull_request_review` - PR review submissions

Only comments starting with `xagent:` are processed.

### Jira Events

The Lambda function processes:

- `comment_created` - New comments on issues

Only comments starting with `xagent:` are processed.

## Monitoring and Debugging

### CloudWatch Logs

Lambda function logs are available in CloudWatch:

```bash
aws logs tail /aws/lambda/xagent-webhooks --follow
```

## Troubleshooting

### Lambda Function Errors

1. **Authentication errors**: Verify webhook secrets match between Terraform and webhook configuration
2. **RPC connection errors**: Verify `XAGENT_SERVER` URL is correct and reachable from Lambda
3. **API key errors**: Verify `XAGENT_TOKEN` is a valid, non-expired API key
4. **Timeout errors**: Increase Lambda timeout in Terraform (default: 30s)

### Webhook Delivery Failures

**GitHub:**
- Check webhook delivery history in GitHub settings
- Look for signature verification failures

**Jira:**
- Check webhook configuration in Jira settings
- Verify the Lambda URL is accessible from Jira Cloud

## Cost Estimation

AWS costs for this architecture are minimal:

- **Lambda**: Free tier includes 1M requests/month and 400,000 GB-seconds of compute

For a typical small to medium usage (< 1000 webhooks/day), the cost should remain within the AWS free tier.

## Security Considerations

1. **Webhook Secrets**: Always use strong, randomly-generated secrets
2. **Function URLs**: Currently set to public (NONE authorization) but validated via webhook signatures
3. **API Key**: The Lambda uses an xagent API key (`xat_` token) for authentication
4. **Credentials**: Never commit secrets to version control
