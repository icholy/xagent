import {
	IExecuteFunctions,
	INodeExecutionData,
	INodeType,
	INodeTypeDescription,
	NodeOperationError,
} from 'n8n-workflow';

const TERMINAL_STATUSES = ['COMPLETED', 'FAILED', 'CANCELLED'];

export class Xagent implements INodeType {
	description: INodeTypeDescription = {
		displayName: 'xagent',
		name: 'xagent',
		icon: 'file:xagent.svg',
		group: ['transform'],
		version: 1,
		subtitle: '={{$parameter["operation"]}}',
		description: 'Create and run xagent tasks',
		defaults: { name: 'xagent' },
		inputs: ['main'],
		outputs: ['main'],
		credentials: [{ name: 'xagentApi', required: true }],
		properties: [
			{
				displayName: 'Operation',
				name: 'operation',
				type: 'options',
				noDataExpression: true,
				options: [
					{
						name: 'Create and Wait',
						value: 'createAndWait',
						action: 'Create a task and wait for completion',
					},
					{
						name: 'Create',
						value: 'create',
						action: 'Create a task',
					},
					{
						name: 'Get Details',
						value: 'getDetails',
						action: 'Get task details',
					},
					{
						name: 'Update',
						value: 'update',
						action: 'Add instructions and start a task',
					},
					{
						name: 'Cancel',
						value: 'cancel',
						action: 'Cancel a task',
					},
				],
				default: 'createAndWait',
			},
			// Create fields (shared by create and createAndWait)
			{
				displayName: 'Workspace',
				name: 'workspace',
				type: 'string',
				default: '',
				required: true,
				displayOptions: { show: { operation: ['create', 'createAndWait'] } },
				description: 'Workspace to run the task in',
			},
			{
				displayName: 'Instruction',
				name: 'instruction',
				type: 'string',
				typeOptions: { rows: 4 },
				default: '',
				required: true,
				displayOptions: { show: { operation: ['create', 'createAndWait'] } },
				description: 'The instruction text for the task',
			},
			{
				displayName: 'Name',
				name: 'taskName',
				type: 'string',
				default: '',
				displayOptions: { show: { operation: ['create', 'createAndWait'] } },
				description: 'Optional name for the task',
			},
			{
				displayName: 'Parent Task ID',
				name: 'parentId',
				type: 'number',
				default: 0,
				displayOptions: { show: { operation: ['create', 'createAndWait'] } },
				description: 'Optional parent task ID',
			},
			// Polling config for createAndWait
			{
				displayName: 'Poll Interval (Seconds)',
				name: 'pollInterval',
				type: 'number',
				default: 10,
				displayOptions: { show: { operation: ['createAndWait'] } },
				description: 'How often to check task status',
			},
			{
				displayName: 'Timeout (Seconds)',
				name: 'timeout',
				type: 'number',
				default: 3600,
				displayOptions: { show: { operation: ['createAndWait'] } },
				description: 'Maximum time to wait before failing (0 = no timeout)',
			},
			// Task ID field (shared by getDetails, update, cancel)
			{
				displayName: 'Task ID',
				name: 'taskId',
				type: 'number',
				default: 0,
				required: true,
				displayOptions: { show: { operation: ['getDetails', 'update', 'cancel'] } },
				description: 'The task ID to operate on',
			},
			// Update operation fields
			{
				displayName: 'Instruction',
				name: 'updateInstruction',
				type: 'string',
				typeOptions: { rows: 4 },
				default: '',
				required: true,
				displayOptions: { show: { operation: ['update'] } },
				description: 'Instruction to add to the task',
			},
			{
				displayName: 'Start',
				name: 'start',
				type: 'boolean',
				default: true,
				displayOptions: { show: { operation: ['update'] } },
				description:
					'Whether to start the task after adding instructions (non-interrupting, waits for current run to finish)',
			},
		],
	};

	async execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
		const items = this.getInputData();
		const returnData: INodeExecutionData[] = [];
		const credentials = await this.getCredentials('xagentApi');
		const serverUrl = (credentials.serverUrl as string).replace(/\/$/, '');

		const rpc = async (method: string, body: Record<string, unknown> = {}) => {
			return this.helpers.httpRequest({
				method: 'POST',
				url: `${serverUrl}/xagent.v1.XAgentService/${method}`,
				headers: {
					'Content-Type': 'application/json',
					Authorization: `Bearer ${credentials.apiKey}`,
					'Connect-Protocol-Version': '1',
				},
				body,
				json: true,
			});
		};

		for (let i = 0; i < items.length; i++) {
			const operation = this.getNodeParameter('operation', i) as string;

			if (operation === 'createAndWait') {
				const createBody: Record<string, unknown> = {
					workspace: this.getNodeParameter('workspace', i) as string,
					instructions: [{ text: this.getNodeParameter('instruction', i) as string }],
				};
				const taskName = this.getNodeParameter('taskName', i) as string;
				if (taskName) createBody.name = taskName;
				const parentId = this.getNodeParameter('parentId', i) as number;
				if (parentId) createBody.parent = parentId;

				const createResp = await rpc('CreateTask', createBody);
				const taskId = createResp.task.id;

				const pollInterval = this.getNodeParameter('pollInterval', i) as number;
				const timeout = this.getNodeParameter('timeout', i) as number;
				const startTime = Date.now();

				let details: any;
				while (true) {
					await new Promise((resolve) => setTimeout(resolve, pollInterval * 1000));

					if (timeout > 0 && Date.now() - startTime > timeout * 1000) {
						throw new NodeOperationError(
							this.getNode(),
							`Task ${taskId} did not complete within ${timeout}s`,
							{ itemIndex: i },
						);
					}

					details = await rpc('GetTaskDetails', { id: taskId });
					if (TERMINAL_STATUSES.includes(details.task.status)) {
						break;
					}
				}

				const logsResp = await rpc('ListLogs', { task_id: taskId });
				returnData.push({
					json: { ...details, logs: logsResp.entries || [] },
					pairedItem: { item: i },
				});
			} else if (operation === 'create') {
				const body: Record<string, unknown> = {
					workspace: this.getNodeParameter('workspace', i) as string,
					instructions: [{ text: this.getNodeParameter('instruction', i) as string }],
				};
				const taskName = this.getNodeParameter('taskName', i) as string;
				if (taskName) body.name = taskName;
				const parentId = this.getNodeParameter('parentId', i) as number;
				if (parentId) body.parent = parentId;

				const resp = await rpc('CreateTask', body);
				returnData.push({ json: resp, pairedItem: { item: i } });
			} else if (operation === 'getDetails') {
				const taskId = this.getNodeParameter('taskId', i) as number;
				const details = await rpc('GetTaskDetails', { id: taskId });
				const logsResp = await rpc('ListLogs', { task_id: taskId });
				returnData.push({
					json: { ...details, logs: logsResp.entries || [] },
					pairedItem: { item: i },
				});
			} else if (operation === 'update') {
				const body: Record<string, unknown> = {
					id: this.getNodeParameter('taskId', i) as number,
					add_instructions: [
						{ text: this.getNodeParameter('updateInstruction', i) as string },
					],
					start: this.getNodeParameter('start', i) as boolean,
				};
				const resp = await rpc('UpdateTask', body);
				returnData.push({ json: resp, pairedItem: { item: i } });
			} else if (operation === 'cancel') {
				const taskId = this.getNodeParameter('taskId', i) as number;
				const resp = await rpc('CancelTask', { id: taskId });
				returnData.push({ json: resp, pairedItem: { item: i } });
			}
		}

		return [returnData];
	}
}
