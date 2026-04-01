# Runner Example

Run an xagent runner using Docker Compose with a pull-through registry cache.

## Prerequisites

- Docker with [sysbox](https://github.com/nestybox/sysbox) runtime v0.7.0+
- An API key from https://xagent.choly.ca/ui/keys

## Setup

Create a `.env` file with your API key:

```bash
echo "XAGENT_API_KEY=your-api-key" > .env
```

## Usage

Start the runner:

```bash
docker compose up
```

This starts:
- A Docker registry pull-through cache (avoids re-downloading images)
- The xagent runner (polls the server for tasks and manages agent containers)

Agent containers are created by the runner on the host Docker daemon. They join the compose network to access the registry cache and use the sysbox runtime for Docker-in-Docker support.
