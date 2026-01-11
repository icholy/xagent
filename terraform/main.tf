terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    sops = {
      source  = "carlpett/sops"
      version = "~> 1.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

provider "sops" {}

# Load secrets from SOPS encrypted file
data "sops_file" "secrets" {
  source_file = "${path.module}/secrets.yaml"
}

# SQS Queue for xagent events
resource "aws_sqs_queue" "xagent_events" {
  name                       = "${var.project_name}-events"
  visibility_timeout_seconds = 60
  message_retention_seconds  = 1209600 # 14 days
  receive_wait_time_seconds  = 20      # Enable long polling

  tags = {
    Name    = "${var.project_name}-events"
    Project = var.project_name
  }
}

# Dead Letter Queue for failed events
resource "aws_sqs_queue" "xagent_events_dlq" {
  name                      = "${var.project_name}-events-dlq"
  message_retention_seconds = 1209600 # 14 days

  tags = {
    Name    = "${var.project_name}-events-dlq"
    Project = var.project_name
  }
}

# Redrive policy to send failed messages to DLQ
resource "aws_sqs_queue_redrive_policy" "xagent_events" {
  queue_url = aws_sqs_queue.xagent_events.id

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.xagent_events_dlq.arn
    maxReceiveCount     = 3
  })
}

# IAM role for Lambda functions
resource "aws_iam_role" "lambda_exec" {
  name = "${var.project_name}-lambda-exec"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "lambda.amazonaws.com"
      }
    }]
  })

  tags = {
    Name    = "${var.project_name}-lambda-exec"
    Project = var.project_name
  }
}

# IAM policy for Lambda to write to SQS
resource "aws_iam_role_policy" "lambda_sqs" {
  name = "${var.project_name}-lambda-sqs"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "sqs:SendMessage",
        "sqs:GetQueueUrl"
      ]
      Resource = aws_sqs_queue.xagent_events.arn
    }]
  })
}

# Attach basic Lambda execution role
resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Build webhooks Lambda function
data "archive_file" "webhooks_lambda" {
  type        = "zip"
  source_dir  = "${path.module}/../lambda"
  output_path = "${path.module}/builds/webhooks-lambda.zip"
  excludes    = ["go.sum", "Makefile", "README.md", ".gitignore", "webhooks"]
}

# Webhooks Lambda function
resource "aws_lambda_function" "webhooks" {
  filename         = data.archive_file.webhooks_lambda.output_path
  function_name    = "${var.project_name}-webhooks"
  role             = aws_iam_role.lambda_exec.arn
  handler          = "bootstrap"
  source_code_hash = data.archive_file.webhooks_lambda.output_base64sha256
  runtime          = "provided.al2023"
  timeout          = 30

  environment {
    variables = {
      SQS_QUEUE_URL         = aws_sqs_queue.xagent_events.url
      GITHUB_WEBHOOK_SECRET = data.sops_file.secrets.data["github_webhook_secret"]
      JIRA_WEBHOOK_SECRET   = data.sops_file.secrets.data["jira_webhook_secret"]
      JIRA_BASE_URL         = data.sops_file.secrets.data["jira_base_url"]
    }
  }

  tags = {
    Name    = "${var.project_name}-webhooks"
    Project = var.project_name
  }
}

# Webhooks Lambda Function URL
resource "aws_lambda_function_url" "webhooks" {
  function_name      = aws_lambda_function.webhooks.function_name
  authorization_type = "NONE"
}
