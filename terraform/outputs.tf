output "sqs_queue_url" {
  description = "URL of the SQS queue for xagent events"
  value       = aws_sqs_queue.xagent_events.url
}

output "sqs_queue_arn" {
  description = "ARN of the SQS queue for xagent events"
  value       = aws_sqs_queue.xagent_events.arn
}

output "sqs_dlq_url" {
  description = "URL of the dead letter queue"
  value       = aws_sqs_queue.xagent_events_dlq.url
}

output "github_webhook_url" {
  description = "GitHub webhook URL to configure in GitHub repository settings"
  value       = aws_lambda_function_url.github_webhook.function_url
}

output "jira_webhook_url" {
  description = "Jira webhook URL to configure in Jira webhook settings"
  value       = aws_lambda_function_url.jira_webhook.function_url
}

output "github_lambda_function_name" {
  description = "Name of the GitHub Lambda function"
  value       = aws_lambda_function.github_webhook.function_name
}

output "jira_lambda_function_name" {
  description = "Name of the Jira Lambda function"
  value       = aws_lambda_function.jira_webhook.function_name
}
