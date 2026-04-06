variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "m5.xlarge"
}

variable "arch" {
  description = "CPU architecture (amd64 or arm64)"
  type        = string
  default     = "amd64"

  validation {
    condition     = contains(["amd64", "arm64"], var.arch)
    error_message = "arch must be amd64 or arm64"
  }
}

variable "key_name" {
  description = "SSH key pair name (optional, for debugging)"
  type        = string
  default     = null
}

variable "vpc_id" {
  description = "VPC ID to launch the instance in"
  type        = string
}

variable "subnet_id" {
  description = "Subnet ID to launch the instance in"
  type        = string
}

variable "root_volume_size" {
  description = "Root EBS volume size in GB"
  type        = number
  default     = 100
}

variable "allow_ssh" {
  description = "Whether to allow SSH access"
  type        = bool
  default     = false
}

variable "ssh_cidr_blocks" {
  description = "CIDR blocks allowed for SSH access"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "xagent_server" {
  description = "xagent server URL"
  type        = string
}

variable "xagent_api_key" {
  description = "xagent API key"
  type        = string
  sensitive   = true
}

variable "xagent_runner_id" {
  description = "Runner identifier"
  type        = string
  default     = "ec2-runner"
}

variable "env_file_content" {
  description = "Additional environment variables to pass to the runner (contents of .env file)"
  type        = string
  default     = ""
  sensitive   = true
}
