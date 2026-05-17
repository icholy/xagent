# n8n-nodes-xagent

n8n community node for [xagent](https://github.com/icholy/xagent) task orchestration.

## Operations

| Operation   | Description                                        |
|-------------|----------------------------------------------------|
| Create      | Create a task in a workspace                       |
| Get Details | Get full task details including logs               |
| Update      | Add instructions to a task and optionally start it |
| Cancel      | Cancel a running task                              |
| Archive     | Archive a task                                     |

Create, Update, and Archive support an optional **Wait for Completion** toggle that polls the task until it reaches a terminal status before returning.

## Credentials

The node authenticates using an xagent API key. You can create one from the xagent web UI or via the `xagent` CLI.

Configure the credential with:
- **Server URL**: Your xagent server URL (e.g. `https://xagent.example.com`)
- **API Key**: An API key generated from xagent

## Development

```bash
pnpm install
pnpm run build
```

To test locally, link the package into your n8n installation:

```bash
pnpm link --global
cd /path/to/n8n
pnpm link --global n8n-nodes-xagent
```
