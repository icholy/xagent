output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.runner.id
}

output "public_ip" {
  description = "Public IP address (if assigned)"
  value       = aws_instance.runner.public_ip
}

output "private_ip" {
  description = "Private IP address"
  value       = aws_instance.runner.private_ip
}
