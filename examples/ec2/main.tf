terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    cloudinit = {
      source  = "hashicorp/cloudinit"
      version = "~> 2.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-${var.arch}-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

locals {
  env_lines = compact(concat(
    [
      "XAGENT_SERVER=${var.xagent_server}",
      "XAGENT_API_KEY=${var.xagent_api_key}",
      "XAGENT_RUNNER_ID=${var.xagent_runner_id}",
    ],
    var.env_file_content != "" ? split("\n", var.env_file_content) : [],
  ))
}

data "cloudinit_config" "runner" {
  gzip          = false
  base64_encode = false

  part {
    content_type = "text/cloud-config"
    content = templatefile("${path.module}/cloud-init.yaml.tftpl", {
      docker_compose   = file("${path.module}/docker-compose.yml")
      workspaces       = file("${path.module}/config/workspaces.yaml")
      env_file_content = join("\n", local.env_lines)
    })
  }
}

resource "aws_security_group" "runner" {
  name_prefix = "xagent-runner-"
  description = "Security group for xagent runner"
  vpc_id      = var.vpc_id

  # SSH access (optional, for debugging)
  dynamic "ingress" {
    for_each = var.allow_ssh ? [1] : []
    content {
      from_port   = 22
      to_port     = 22
      protocol    = "tcp"
      cidr_blocks = var.ssh_cidr_blocks
      description = "SSH access"
    }
  }

  # All outbound traffic
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "All outbound traffic"
  }

  tags = {
    Name = "xagent-runner"
  }
}

resource "aws_instance" "runner" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type
  key_name               = var.key_name
  vpc_security_group_ids = [aws_security_group.runner.id]
  subnet_id              = var.subnet_id
  user_data              = data.cloudinit_config.runner.rendered

  root_block_device {
    volume_size = var.root_volume_size
    volume_type = "gp3"
  }

  tags = {
    Name = "xagent-runner"
  }
}
