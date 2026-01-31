# Webhook Event Architecture Setup Guide

This guide explains how to set up the webhook-based architecture for xagent using AWS Lambda to receive GitHub and Jira webhooks and submit events directly to the xagent server via RPC.

## Architecture Overview

```
GitHub/Jira Webhook → Lambda Function URL → Transform → xagent server (RPC)
```

### Components

1. **AWS Lambda with Function URLs**
   - Receives GitHub and Jira webhooks
   - Transforms webhook payloads into xagent event structure
   - Submits events directly to the xagent server via RPC using an API key

## Prerequisites

- AWS account with appropriate permissions
- Terraform >= 1.0 installed
- Go 1.23 or later
- Access to GitHub/Jira with admin permissions to configure webhooks
- An xagent API key (`XAGENT_TOKEN`)

## Infrastructure Setup

### 1. Build Lambda Function

Build the Lambda function for deployment:

```bash
cd cmd/webhook
GOOS=linux GOARCH=amd64 go build -tags lambda.norpc -o bootstrap .
cd ../..
```

### 2. Configure Terraform Secrets

The Terraform configuration uses SOPS-encrypted secrets. Ensure the following keys are present in `terraform/secrets.yaml`:

- `aws_region` - AWS region for deployment
- `project_name` - Project name prefix for resources
- `github_webhook_secret` - Secret for GitHub webhook signature verification
- `jira_webhook_secret` - Secret for Jira webhook signature verification
- `jira_base_url` - Base URL for your Jira instance (e.g., `https://your-domain.atlassian.net`)
- `xagent_server` - URL of your xagent server (e.g., `https://xagent.choly.ca`)
- `xagent_token` - API key for authenticating with the xagent server

### 3. Deploy Infrastructure

Initialize and apply Terraform:

```bash
cd terraform
terraform init
terraform plan
terraform apply
```

After successful deployment, note the output values:

- `github_webhook_url` - Configure this in GitHub
- `jira_webhook_url` - Configure this in Jira

## Webhook Configuration

### GitHub Webhook Setup

1. Go to your GitHub repository settings
2. Navigate to **Settings** → **Webhooks** → **Add webhook**
3. Configure the webhook:
   - **Payload URL**: Use the `github_webhook_url` from Terraform output
   - **Content type**: `application/json`
   - **Secret**: Use the same `github_webhook_secret` from secrets.yaml
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
   - **URL**: Use the `jira_webhook_url` from Terraform output
   - **Events**: Select:
     - Issue → commented
5. Click **Create**

## Usage

### Notifying Existing Tasks

Comment on a linked GitHub PR or Jira issue with:

```
xagent: <additional instructions or updates>
```

This will create an event that notifies all tasks linked to that PR/issue.

## Event Flow Details

### GitHub Events

The webhook handler processes the following GitHub events:

- `issue_comment` - Comments on issues and PRs
- `pull_request_review_comment` - Review comments on PRs
- `pull_request_review` - Reviews submitted on PRs

Only comments starting with `xagent:` are processed.

### Jira Events

The webhook handler processes:

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
2. **RPC errors**: Ensure `XAGENT_SERVER` and `XAGENT_TOKEN` are correctly configured
3. **Timeout errors**: Increase Lambda timeout in Terraform (default: 30s)

### Webhook Delivery Failures

**GitHub:**
- Check webhook delivery history in GitHub settings
- Look for signature verification failures

**Jira:**
- Check webhook configuration in Jira settings
- Verify the Lambda URL is accessible from Jira Cloud

## Security Considerations

1. **Webhook Secrets**: Always use strong, randomly-generated secrets
2. **Function URLs**: Currently set to public (NONE authorization) but validated via webhook signatures
3. **API Key**: The `XAGENT_TOKEN` is stored as a Lambda environment variable via SOPS-encrypted secrets
4. **Credentials**: Never commit secrets to version control

## Cost Estimation

AWS costs for this architecture are typically very low:

- **Lambda**: Free tier includes 1M requests/month and 400,000 GB-seconds of compute
- **Data transfer**: Minimal for webhook payloads

For a typical small to medium usage (< 1000 webhooks/day), the cost should remain within the AWS free tier.
