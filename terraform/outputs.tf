output "webhook_base_url" {
  description = "Base URL for webhooks Lambda function"
  value       = aws_lambda_function_url.webhooks.function_url
}

output "github_webhook_url" {
  description = "GitHub webhook URL to configure in GitHub repository settings"
  value       = "${aws_lambda_function_url.webhooks.function_url}webhook/github"
}

output "jira_webhook_url" {
  description = "Jira webhook URL to configure in Jira webhook settings"
  value       = "${aws_lambda_function_url.webhooks.function_url}webhook/jira"
}

output "lambda_function_name" {
  description = "Name of the webhooks Lambda function"
  value       = aws_lambda_function.webhooks.function_name
}
