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
  }
}

provider "aws" {
  region = var.aws_region
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

# Build GitHub Lambda function
data "archive_file" "github_lambda" {
  type        = "zip"
  source_dir  = "${path.module}/../lambda/github"
  output_path = "${path.module}/builds/github-lambda.zip"
  excludes    = ["go.sum"]
}

# GitHub Lambda function
resource "aws_lambda_function" "github_webhook" {
  filename         = data.archive_file.github_lambda.output_path
  function_name    = "${var.project_name}-github-webhook"
  role             = aws_iam_role.lambda_exec.arn
  handler          = "bootstrap"
  source_code_hash = data.archive_file.github_lambda.output_base64sha256
  runtime          = "provided.al2023"
  timeout          = 30

  environment {
    variables = {
      SQS_QUEUE_URL         = aws_sqs_queue.xagent_events.url
      GITHUB_WEBHOOK_SECRET = var.github_webhook_secret
    }
  }

  tags = {
    Name    = "${var.project_name}-github-webhook"
    Project = var.project_name
  }
}

# GitHub Lambda Function URL
resource "aws_lambda_function_url" "github_webhook" {
  function_name      = aws_lambda_function.github_webhook.function_name
  authorization_type = "NONE"
}

# Build Jira Lambda function
data "archive_file" "jira_lambda" {
  type        = "zip"
  source_dir  = "${path.module}/../lambda/jira"
  output_path = "${path.module}/builds/jira-lambda.zip"
  excludes    = ["go.sum"]
}

# Jira Lambda function
resource "aws_lambda_function" "jira_webhook" {
  filename         = data.archive_file.jira_lambda.output_path
  function_name    = "${var.project_name}-jira-webhook"
  role             = aws_iam_role.lambda_exec.arn
  handler          = "bootstrap"
  source_code_hash = data.archive_file.jira_lambda.output_base64sha256
  runtime          = "provided.al2023"
  timeout          = 30

  environment {
    variables = {
      SQS_QUEUE_URL       = aws_sqs_queue.xagent_events.url
      JIRA_WEBHOOK_SECRET = var.jira_webhook_secret
      JIRA_BASE_URL       = var.jira_base_url
    }
  }

  tags = {
    Name    = "${var.project_name}-jira-webhook"
    Project = var.project_name
  }
}

# Jira Lambda Function URL
resource "aws_lambda_function_url" "jira_webhook" {
  function_name      = aws_lambda_function.jira_webhook.function_name
  authorization_type = "NONE"
}
