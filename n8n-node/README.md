# n8n-nodes-xagent

n8n community node for [xagent](https://github.com/icholy/xagent) task orchestration.

## Operations

| Operation       | Description                                          |
|-----------------|------------------------------------------------------|
| Create and Wait | Create a task, poll until completion, return details  |
| Create          | Create a task (fire-and-forget)                       |
| Get Details     | Get full task details including logs                  |
| Update          | Add instructions to a task and optionally start it    |
| Cancel          | Cancel a running task                                 |

## Credentials

The node authenticates using an xagent API key. You can create one from the xagent web UI or via the `xagent` CLI.

Configure the credential with:
- **Server URL**: Your xagent server URL (e.g. `https://xagent.example.com`)
- **API Key**: An API key generated from xagent

## Development

```bash
npm install
npm run build
```

To test locally, link the package into your n8n installation:

```bash
npm link
cd /path/to/n8n
npm link n8n-nodes-xagent
```
