# n8n Community Node for xagent

Issue: https://github.com/icholy/xagent/issues/561

## Problem

There is no way to integrate xagent with n8n workflows. Users who automate processes with n8n cannot create xagent tasks, monitor their status, or react to task completion as part of larger automation pipelines.

## Design

A community node package (`n8n-nodes-xagent`) that exposes xagent's Connect RPC API as n8n node operations. The package uses the **programmatic style** (execute method) since xagent uses Connect RPC (JSON-over-HTTP POST) rather than a traditional REST API with distinct HTTP methods/paths per resource.

### Package Structure

```
n8n-nodes-xagent/
├── package.json
├── tsconfig.json
├── credentials/
│   └── XagentApi.credentials.ts
├── nodes/
│   └── Xagent/
│       ├── Xagent.node.ts
│       └── Xagent.node.json      # codex file (metadata)
└── README.md
```

### Credentials

Authentication uses API keys (issued via `CreateKey` RPC). The credential definition:

```typescript
import { ICredentialType, INodeProperties } from 'n8n-workflow';

export class XagentApi implements ICredentialType {
  name = 'xagentApi';
  displayName = 'xagent API';
  properties: INodeProperties[] = [
    {
      displayName: 'Server URL',
      name: 'serverUrl',
      type: 'string',
      default: '',
      placeholder: 'https://xagent.example.com',
      required: true,
    },
    {
      displayName: 'API Key',
      name: 'apiKey',
      type: 'string',
      typeOptions: { password: true },
      default: '',
      required: true,
    },
  ];
}
```

The node sends requests with `Authorization: Bearer <apiKey>` header, matching xagent's existing key auth.

### Node Resources and Operations

The node exposes a **Resource** dropdown (Task, Link, Event, Workspace, Log) and an **Operation** dropdown per resource:

| Resource  | Operation        | RPC Method       | Description                        |
|-----------|------------------|------------------|------------------------------------|
| Task      | Create           | CreateTask       | Create a new task                  |
| Task      | Get              | GetTask          | Get a task by ID                   |
| Task      | Get Details      | GetTaskDetails   | Get task with children/events/links|
| Task      | List             | ListTasks        | List all tasks                     |
| Task      | List Children    | ListChildTasks   | List child tasks of a parent       |
| Task      | Update           | UpdateTask       | Update name or add instructions    |
| Task      | Cancel           | CancelTask       | Cancel a running task              |
| Task      | Restart          | RestartTask      | Restart a task                     |
| Task      | Archive          | ArchiveTask      | Archive a task                     |
| Link      | Create           | CreateLink       | Create a link on a task            |
| Link      | List             | ListLinks        | List links for a task              |
| Event     | Create           | CreateEvent      | Create an event                    |
| Event     | List             | ListEvents       | List recent events                 |
| Event     | List By Task     | ListEventsByTask | List events for a task             |
| Workspace | List             | ListWorkspaces   | List available workspaces          |
| Log       | List             | ListLogs         | List logs for a task               |

### Connect RPC HTTP Mapping

xagent uses Connect RPC which exposes unary RPCs as HTTP POST endpoints:

```
POST /xagent.v1.XAgentService/<MethodName>
Content-Type: application/json
Authorization: Bearer <key>

{ ...request fields... }
```

For example, `CreateTask` maps to:

```
POST /xagent.v1.XAgentService/CreateTask
Content-Type: application/json

{
  "name": "Deploy staging",
  "workspace": "default",
  "instructions": [{ "text": "Deploy the app to staging" }]
}
```

### Execute Method Implementation

The core execute logic dispatches based on resource/operation and calls the Connect RPC endpoint:

```typescript
import {
  IExecuteFunctions,
  INodeExecutionData,
  INodeType,
  INodeTypeDescription,
} from 'n8n-workflow';

export class Xagent implements INodeType {
  description: INodeTypeDescription = {
    displayName: 'xagent',
    name: 'xagent',
    icon: 'file:xagent.svg',
    group: ['transform'],
    version: 1,
    subtitle: '={{$parameter["operation"] + ": " + $parameter["resource"]}}',
    description: 'Interact with xagent tasks and workflows',
    defaults: { name: 'xagent' },
    inputs: ['main'],
    outputs: ['main'],
    credentials: [
      { name: 'xagentApi', required: true },
    ],
    properties: [
      // Resource selector
      {
        displayName: 'Resource',
        name: 'resource',
        type: 'options',
        noDataExpression: true,
        options: [
          { name: 'Task', value: 'task' },
          { name: 'Link', value: 'link' },
          { name: 'Event', value: 'event' },
          { name: 'Workspace', value: 'workspace' },
          { name: 'Log', value: 'log' },
        ],
        default: 'task',
      },
      // Task operations
      {
        displayName: 'Operation',
        name: 'operation',
        type: 'options',
        noDataExpression: true,
        displayOptions: { show: { resource: ['task'] } },
        options: [
          { name: 'Create', value: 'create', action: 'Create a task' },
          { name: 'Get', value: 'get', action: 'Get a task' },
          { name: 'Get Details', value: 'getDetails', action: 'Get task details' },
          { name: 'List', value: 'list', action: 'List tasks' },
          { name: 'List Children', value: 'listChildren', action: 'List child tasks' },
          { name: 'Update', value: 'update', action: 'Update a task' },
          { name: 'Cancel', value: 'cancel', action: 'Cancel a task' },
          { name: 'Restart', value: 'restart', action: 'Restart a task' },
          { name: 'Archive', value: 'archive', action: 'Archive a task' },
        ],
        default: 'create',
      },
      // ... additional operation selectors for other resources ...
      // Task Create fields
      {
        displayName: 'Workspace',
        name: 'workspace',
        type: 'string',
        default: '',
        required: true,
        displayOptions: { show: { resource: ['task'], operation: ['create'] } },
        description: 'Workspace to run the task in',
      },
      {
        displayName: 'Instruction',
        name: 'instruction',
        type: 'string',
        typeOptions: { rows: 4 },
        default: '',
        required: true,
        displayOptions: { show: { resource: ['task'], operation: ['create'] } },
        description: 'The instruction text for the task',
      },
      {
        displayName: 'Name',
        name: 'taskName',
        type: 'string',
        default: '',
        displayOptions: { show: { resource: ['task'], operation: ['create'] } },
        description: 'Optional name for the task',
      },
      {
        displayName: 'Parent Task ID',
        name: 'parentId',
        type: 'number',
        default: 0,
        displayOptions: { show: { resource: ['task'], operation: ['create'] } },
        description: 'Optional parent task ID',
      },
      // Task ID field (shared by get, cancel, restart, archive, etc.)
      {
        displayName: 'Task ID',
        name: 'taskId',
        type: 'number',
        default: 0,
        required: true,
        displayOptions: {
          show: {
            resource: ['task'],
            operation: ['get', 'getDetails', 'cancel', 'restart', 'archive', 'update', 'listChildren'],
          },
        },
      },
      // ... additional fields for other operations ...
    ],
  };

  async execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
    const items = this.getInputData();
    const returnData: INodeExecutionData[] = [];
    const credentials = await this.getCredentials('xagentApi');
    const serverUrl = (credentials.serverUrl as string).replace(/\/$/, '');

    for (let i = 0; i < items.length; i++) {
      const resource = this.getNodeParameter('resource', i) as string;
      const operation = this.getNodeParameter('operation', i) as string;

      let method: string;
      let body: Record<string, unknown> = {};

      if (resource === 'task') {
        switch (operation) {
          case 'create':
            method = 'CreateTask';
            body = {
              name: this.getNodeParameter('taskName', i) as string,
              workspace: this.getNodeParameter('workspace', i) as string,
              instructions: [{ text: this.getNodeParameter('instruction', i) as string }],
            };
            const parentId = this.getNodeParameter('parentId', i) as number;
            if (parentId) body.parent = parentId;
            break;
          case 'get':
            method = 'GetTask';
            body = { id: this.getNodeParameter('taskId', i) };
            break;
          case 'getDetails':
            method = 'GetTaskDetails';
            body = { id: this.getNodeParameter('taskId', i) };
            break;
          case 'list':
            method = 'ListTasks';
            break;
          case 'listChildren':
            method = 'ListChildTasks';
            body = { parent_id: this.getNodeParameter('taskId', i) };
            break;
          case 'cancel':
            method = 'CancelTask';
            body = { id: this.getNodeParameter('taskId', i) };
            break;
          case 'restart':
            method = 'RestartTask';
            body = { id: this.getNodeParameter('taskId', i) };
            break;
          case 'archive':
            method = 'ArchiveTask';
            body = { id: this.getNodeParameter('taskId', i) };
            break;
          // ... update, etc.
        }
      }
      // ... other resources follow same pattern ...

      const response = await this.helpers.httpRequest({
        method: 'POST',
        url: `${serverUrl}/xagent.v1.XAgentService/${method}`,
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${credentials.apiKey}`,
          'Connect-Protocol-Version': '1',
        },
        body,
        json: true,
      });

      returnData.push({ json: response, pairedItem: { item: i } });
    }

    return [returnData];
  }
}
```

### Trigger Node (Polling)

A separate **trigger node** (`XagentTrigger`) enables workflows to start when a task reaches a specific status. Since xagent doesn't have outbound webhooks to arbitrary URLs, the trigger uses polling:

```typescript
export class XagentTrigger implements INodeType {
  description: INodeTypeDescription = {
    displayName: 'xagent Trigger',
    name: 'xagentTrigger',
    icon: 'file:xagent.svg',
    group: ['trigger'],
    version: 1,
    polling: true,
    description: 'Triggers when a task changes status',
    defaults: { name: 'xagent Trigger' },
    inputs: [],
    outputs: ['main'],
    credentials: [{ name: 'xagentApi', required: true }],
    properties: [
      {
        displayName: 'Task ID',
        name: 'taskId',
        type: 'number',
        default: 0,
        required: true,
        description: 'Task to monitor',
      },
      {
        displayName: 'Trigger On Status',
        name: 'triggerStatus',
        type: 'options',
        options: [
          { name: 'Completed', value: 'COMPLETED' },
          { name: 'Failed', value: 'FAILED' },
          { name: 'Cancelled', value: 'CANCELLED' },
          { name: 'Any Terminal', value: 'ANY_TERMINAL' },
        ],
        default: 'COMPLETED',
      },
    ],
  };

  async poll(this: IExecuteFunctions): Promise<INodeExecutionData[][] | null> {
    // Poll GetTask, compare status against triggerStatus
    // Return data when matched, null otherwise
  }
}
```

### Alternative: Webhook Trigger via xagent Events

Instead of polling, a more advanced approach would add a generic webhook endpoint to xagent that creates events when hit. The n8n trigger would:

1. On activation: register a webhook URL as an event source in xagent
2. Receive POST callbacks when task status changes
3. On deactivation: unregister the webhook

This would require server-side changes (a new `RegisterWebhook` RPC) and is out of scope for the initial version.

### Package Metadata

```json
{
  "name": "n8n-nodes-xagent",
  "version": "0.1.0",
  "description": "n8n community node for xagent task orchestration",
  "keywords": ["n8n-community-node-package"],
  "license": "MIT",
  "n8n": {
    "n8nNodesApiVersion": 1,
    "credentials": ["dist/credentials/XagentApi.credentials.js"],
    "nodes": [
      "dist/nodes/Xagent/Xagent.node.js",
      "dist/nodes/Xagent/XagentTrigger.node.js"
    ]
  }
}
```

### Repository Location

The node lives in a separate repository (`icholy/n8n-nodes-xagent`) since it's an npm package with its own release cycle. It has no dependency on the xagent Go codebase.

## Trade-offs

**Programmatic vs. Declarative style**: Chose programmatic because Connect RPC uses a single URL pattern (`POST /service/Method`) with method-specific JSON bodies. The declarative style works best with REST APIs that use distinct HTTP methods and URL paths per operation.

**Polling trigger vs. Webhook trigger**: Chose polling for the initial version because it requires no server-side changes. The downside is latency (bounded by poll interval, default 1 minute in n8n). A webhook-based trigger would be more efficient but requires adding webhook registration to the xagent server.

**Separate repo vs. monorepo**: The n8n node is a TypeScript/npm package with no shared code with the Go server. Keeping it separate avoids polluting the Go module and lets it follow npm publishing conventions independently.

**Flat operations vs. sub-nodes**: n8n supports "sub-nodes" for complex integrations, but a single node with resource/operation dropdowns is simpler, well-understood by n8n users, and sufficient for xagent's API surface.

## Open Questions

1. **Should the trigger node support watching multiple tasks?** A "List Tasks" poll mode that triggers on any new terminal task could be more useful for general-purpose automation than watching a single task ID.

2. **Should we add a webhook endpoint to xagent?** A generic `/api/webhooks` endpoint that fires on task status changes would enable real-time n8n triggers without polling. This would benefit other integrations beyond n8n.

3. **SSE-based trigger**: xagent already has SSE for the web UI. Could the n8n trigger use SSE instead of polling? n8n's trigger model expects poll-or-webhook, so this would require a custom approach.

4. **Verified node submission**: n8n requires zero runtime dependencies for verified nodes. Since the node only uses `this.helpers.httpRequest()` (built-in), this constraint is satisfied. Should we pursue verification?
