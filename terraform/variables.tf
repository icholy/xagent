variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "xagent"
}

variable "github_webhook_secret" {
  description = "Secret for GitHub webhook signature verification"
  type        = string
  sensitive   = true
}

variable "jira_webhook_secret" {
  description = "Secret for Jira webhook signature verification"
  type        = string
  sensitive   = true
}

variable "jira_base_url" {
  description = "Base URL for Jira instance (e.g., https://your-domain.atlassian.net)"
  type        = string
}
