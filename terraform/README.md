# Terraform Infrastructure for xagent Webhooks

This directory contains Terraform configuration for deploying the webhook-based architecture for xagent.

## Overview

The infrastructure includes:

- **SQS Queue**: Message queue for webhook events
- **Dead Letter Queue**: Failed event handling
- **GitHub Lambda**: Processes GitHub webhook events
- **Jira Lambda**: Processes Jira webhook events
- **IAM Roles**: Execution roles and permissions
- **Lambda Function URLs**: Public endpoints for webhooks

## Prerequisites

1. **AWS Account** with appropriate permissions
2. **Terraform** >= 1.0 installed
3. **AWS CLI** configured with credentials
4. **Lambda Functions** built (see `../lambda/README.md`)
5. **SOPS** installed for secrets management (https://github.com/getsops/sops)

## Quick Start

### 1. Build Lambda Functions

First, build the Lambda functions:

```bash
cd ../lambda
make all
cd ../terraform
```

### 2. Configure Secrets

Edit the encrypted secrets file using SOPS:

```bash
sops secrets.yaml
```

Example configuration:

```yaml
aws_region: "us-east-1"
project_name: "xagent"
github_webhook_secret: "your-github-webhook-secret-here"
jira_webhook_secret: "your-jira-webhook-secret-here"
jira_base_url: "https://your-domain.atlassian.net"
```

### 3. Deploy Infrastructure

```bash
# Initialize Terraform
terraform init

# Review the plan
terraform plan

# Apply the configuration
terraform apply
```

### 4. Save Outputs

After successful deployment, save the important outputs:

```bash
# Get all outputs
terraform output

# Save specific outputs
terraform output -raw sqs_queue_url > queue_url.txt
terraform output -raw github_webhook_url > github_webhook.txt
terraform output -raw jira_webhook_url > jira_webhook.txt
```

## Configuration Reference

### Secrets (secrets.yaml)

All configuration is stored in `secrets.yaml` and managed with SOPS:

| Key | Description | Required |
|-----|-------------|----------|
| `aws_region` | AWS region for resources | Yes |
| `project_name` | Project name for resource naming | Yes |
| `github_webhook_secret` | GitHub webhook signature secret | Yes |
| `jira_webhook_secret` | Jira webhook signature secret | Yes |
| `jira_base_url` | Jira base URL | Yes |

### Outputs

| Output | Description |
|--------|-------------|
| `sqs_queue_url` | SQS queue URL for xagent subscribe |
| `sqs_queue_arn` | SQS queue ARN |
| `sqs_dlq_url` | Dead letter queue URL |
| `github_webhook_url` | URL to configure in GitHub |
| `jira_webhook_url` | URL to configure in Jira |
| `github_lambda_function_name` | GitHub Lambda function name |
| `jira_lambda_function_name` | Jira Lambda function name |

## Infrastructure Components

### SQS Queue

- **Name**: `{project_name}-events`
- **Visibility Timeout**: 60 seconds
- **Message Retention**: 14 days
- **Long Polling**: Enabled (20 seconds)
- **Redrive Policy**: 3 retries before moving to DLQ

### Lambda Functions

Both Lambda functions use:

- **Runtime**: `provided.al2023` (custom Go runtime)
- **Timeout**: 30 seconds
- **Memory**: Default (128 MB)
- **Architecture**: AMD64

Environment variables are automatically configured from Terraform variables.

### IAM Permissions

Lambda functions have minimal permissions:

- **CloudWatch Logs**: Write logs
- **SQS**: `SendMessage` to the event queue

## Updating Lambda Functions

To update Lambda function code:

```bash
# Rebuild Lambda functions
cd ../lambda
make clean
make all

# Update infrastructure
cd ../terraform
terraform apply
```

Terraform will detect the source code hash change and update the Lambda functions.

## Monitoring

### CloudWatch Logs

View Lambda logs:

```bash
# GitHub Lambda logs
aws logs tail /aws/lambda/xagent-github-webhook --follow

# Jira Lambda logs
aws logs tail /aws/lambda/xagent-jira-webhook --follow
```

### SQS Queue Metrics

Check queue statistics:

```bash
aws sqs get-queue-attributes \
  --queue-url $(terraform output -raw sqs_queue_url) \
  --attribute-names All
```

### Dead Letter Queue

Check for failed messages:

```bash
aws sqs receive-message \
  --queue-url $(terraform output -raw sqs_dlq_url) \
  --max-number-of-messages 10
```

## Cost Estimation

Typical monthly costs for moderate usage (< 10,000 events/month):

- **Lambda**: $0 (within free tier: 1M requests)
- **SQS**: $0 (within free tier: 1M requests)
- **CloudWatch Logs**: ~$0.50 (for log storage)
- **Data Transfer**: Negligible

**Total**: < $1/month for typical usage

## Troubleshooting

### Lambda Deployment Issues

**Problem**: Lambda function fails to deploy

```bash
# Check build artifacts exist
ls -lh builds/

# Verify Lambda function package
unzip -l builds/github-lambda.zip
```

**Solution**: Rebuild Lambda functions using `make all` in the lambda directory.

### Permission Errors

**Problem**: Lambda can't send to SQS

**Solution**: Verify IAM role has SQS permissions:

```bash
terraform plan
# Look for aws_iam_role_policy.lambda_sqs
```

### Webhook Signature Verification Fails

**Problem**: GitHub/Jira webhooks fail with 401

**Solution**: Ensure webhook secrets match:

1. Check `secrets.yaml` values (use `sops secrets.yaml` to view/edit)
2. Verify GitHub/Jira webhook configuration
3. Re-apply Terraform to update Lambda environment variables

## Security Considerations

### Secrets Management

Secrets are managed using [SOPS](https://github.com/getsops/sops). Use `sops secrets.yaml` to edit the encrypted file.

### Function URLs

Lambda Function URLs are currently public but protected by:

- **GitHub**: HMAC-SHA256 signature verification
- **Jira**: HMAC-SHA256 with Base64 encoding

Consider adding:

- API Gateway with WAF rules
- AWS CloudFront with origin access control
- Rate limiting via AWS WAF

### Network Security

The current setup uses:

- Public Lambda Function URLs (no VPC)
- Public SQS endpoints

For enhanced security, consider:

- VPC endpoints for SQS
- Private API Gateway
- VPC Lambda functions

## Cleanup

To destroy all infrastructure:

```bash
terraform destroy
```

**Warning**: This will delete:
- All Lambda functions
- SQS queues (and messages)
- CloudWatch log groups
- IAM roles and policies

## Next Steps

After deploying infrastructure:

1. Configure GitHub webhooks (see `../docs/WEBHOOK_SETUP.md`)
2. Configure Jira webhooks (see `../docs/WEBHOOK_SETUP.md`)
3. Start the xagent subscribe command
4. Test the webhook flow

## Additional Resources

- [AWS Lambda Documentation](https://docs.aws.amazon.com/lambda/)
- [AWS SQS Documentation](https://docs.aws.amazon.com/sqs/)
- [Terraform AWS Provider](https://registry.terraform.io/providers/hashicorp/aws/latest/docs)
- [GitHub Webhook Documentation](https://docs.github.com/webhooks)
- [Jira Webhook Documentation](https://developer.atlassian.com/cloud/jira/platform/webhooks/)
