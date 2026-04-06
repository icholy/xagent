# EC2 Runner Example

Deploy an xagent runner to AWS EC2 using Terraform. The instance is provisioned with cloud-init which installs Docker, Sysbox, and starts the runner via Docker Compose.

## What gets deployed

- An EC2 instance (Ubuntu 24.04) with:
  - Docker Engine and Docker Compose
  - [Sysbox](https://github.com/nestybox/sysbox) runtime for Docker-in-Docker support
  - A Docker Compose stack running:
    - The xagent runner (polls the server for tasks)
    - A Docker registry pull-through cache (avoids re-downloading images)
- A security group with outbound-only access (SSH optional)

## Prerequisites

- Terraform >= 1.0
- AWS credentials configured
- An existing VPC and subnet
- An xagent API key from your server's `/ui/keys` page

## Usage

```bash
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your values

terraform init
terraform apply
```

## Configuration

Edit `config/workspaces.yaml` to configure which workspaces the runner supports. Edit `docker-compose.yml` to customize the compose stack. Both files are deployed to the instance via cloud-init.

To pass additional environment variables (e.g. `CLAUDE_CODE_OAUTH_TOKEN`), use the `env_file_content` variable:

```hcl
env_file_content = "CLAUDE_CODE_OAUTH_TOKEN=your-token"
```

## Debugging

Enable SSH access by setting `allow_ssh = true` and providing a `key_name`. Cloud-init logs are at `/var/log/cloud-init-output.log`. The Docker Compose stack is in `/opt/xagent/`.
