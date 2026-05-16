import {
	IExecuteFunctions,
	INodeExecutionData,
	INodeType,
	INodeTypeDescription,
	NodeApiError,
	NodeOperationError,
} from 'n8n-workflow';
import { createClient, ConnectError, type Interceptor } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { toJson } from '@bufbuild/protobuf';
import {
	XAgentService,
	CreateTaskResponseSchema,
	GetTaskDetailsResponseSchema,
	ListLogsResponseSchema,
	UpdateTaskResponseSchema,
	CancelTaskResponseSchema,
} from '../../gen/xagent/v1/xagent_pb';

const TERMINAL_STATUSES = ['COMPLETED', 'FAILED', 'CANCELLED'];

export class Xagent implements INodeType {
	description: INodeTypeDescription = {
		displayName: 'xagent',
		name: 'xagent',
		icon: 'file:xagent.png',
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
				default: 'create',
			},
			// Create fields
			{
				displayName: 'Runner',
				name: 'runner',
				type: 'string',
				default: '',
				required: true,
				displayOptions: { show: { operation: ['create'] } },
				description: 'Runner ID that should handle this task',
			},
			{
				displayName: 'Workspace',
				name: 'workspace',
				type: 'string',
				default: '',
				required: true,
				displayOptions: { show: { operation: ['create'] } },
				description: 'Workspace to run the task in',
			},
			{
				displayName: 'Instruction',
				name: 'instruction',
				type: 'string',
				typeOptions: { rows: 4 },
				default: '',
				required: true,
				displayOptions: { show: { operation: ['create'] } },
				description: 'The instruction text for the task',
			},
			{
				displayName: 'Name',
				name: 'taskName',
				type: 'string',
				default: '',
				displayOptions: { show: { operation: ['create'] } },
				description: 'Optional name for the task',
			},
			{
				displayName: 'Parent Task ID',
				name: 'parentId',
				type: 'number',
				default: 0,
				displayOptions: { show: { operation: ['create'] } },
				description: 'Optional parent task ID',
			},
			{
				displayName: 'Wait for Completion',
				name: 'waitForCompletion',
				type: 'boolean',
				default: true,
				displayOptions: { show: { operation: ['create'] } },
				description: 'Whether to poll the task until it reaches a terminal status before returning',
			},
			{
				displayName: 'Poll Interval (Seconds)',
				name: 'pollInterval',
				type: 'number',
				default: 10,
				displayOptions: { show: { operation: ['create'], waitForCompletion: [true] } },
				description: 'How often to check task status',
			},
			{
				displayName: 'Timeout (Seconds)',
				name: 'timeout',
				type: 'number',
				default: 3600,
				displayOptions: { show: { operation: ['create'], waitForCompletion: [true] } },
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
		const apiKey = credentials.apiKey as string;

		const authInterceptor: Interceptor = (next) => async (req) => {
			req.header.set('Authorization', `Bearer ${apiKey}`);
			req.header.set('X-Auth-Type', 'key');
			return next(req);
		};

		const transport = createConnectTransport({
			baseUrl: serverUrl,
			interceptors: [authInterceptor],
		});

		const client = createClient(XAgentService, transport);

		const rpc = async <T>(method: string, fn: () => Promise<T>): Promise<T> => {
			try {
				return await fn();
			} catch (err) {
				if (err instanceof ConnectError) {
					throw new NodeApiError(this.getNode(), {}, {
						message: `${method}: ${err.message}`,
					});
				}
				throw err;
			}
		};

		for (let i = 0; i < items.length; i++) {
			const operation = this.getNodeParameter('operation', i) as string;

			if (operation === 'create') {
				const runner = this.getNodeParameter('runner', i) as string;
				const workspace = this.getNodeParameter('workspace', i) as string;
				const instruction = this.getNodeParameter('instruction', i) as string;
				const taskName = this.getNodeParameter('taskName', i) as string;
				const parentId = this.getNodeParameter('parentId', i) as number;

				const createResp = await rpc('CreateTask', () =>
					client.createTask({
						runner,
						workspace,
						instructions: [{ text: instruction }],
						name: taskName || undefined,
						parent: parentId ? BigInt(parentId) : undefined,
					}),
				);
				const createJson = toJson(CreateTaskResponseSchema, createResp) as any;

				const waitForCompletion = this.getNodeParameter('waitForCompletion', i) as boolean;
				if (!waitForCompletion) {
					returnData.push({ json: createJson, pairedItem: { item: i } });
					continue;
				}

				const taskId = createResp.task!.id;
				const pollInterval = this.getNodeParameter('pollInterval', i) as number;
				const timeout = this.getNodeParameter('timeout', i) as number;
				const startTime = Date.now();

				let detailsJson: any;
				let detailsResp: any;
				while (true) {
					await new Promise((resolve) => setTimeout(resolve, pollInterval * 1000));

					if (timeout > 0 && Date.now() - startTime > timeout * 1000) {
						throw new NodeOperationError(
							this.getNode(),
							`Task ${taskId} did not complete within ${timeout}s`,
							{ itemIndex: i },
						);
					}

					detailsResp = await rpc('GetTaskDetails', () =>
						client.getTaskDetails({ id: taskId }),
					);
					detailsJson = toJson(GetTaskDetailsResponseSchema, detailsResp) as any;
					if (TERMINAL_STATUSES.includes(detailsJson.task.status)) {
						break;
					}
				}

				const logsResp = await rpc('ListLogs', () =>
					client.listLogs({ taskId }),
				);
				const logsJson = toJson(ListLogsResponseSchema, logsResp) as any;
				if (detailsJson.task.status === 'FAILED') {
					throw new NodeOperationError(
						this.getNode(),
						`Task ${taskId} ended with status FAILED`,
						{ itemIndex: i },
					);
				}
				returnData.push({
					json: { ...detailsJson, logs: logsJson.entries || [] },
					pairedItem: { item: i },
				});
			} else if (operation === 'getDetails') {
				const taskId = this.getNodeParameter('taskId', i) as number;
				const detailsResp = await rpc('GetTaskDetails', () =>
					client.getTaskDetails({ id: BigInt(taskId) }),
				);
				const detailsJson = toJson(GetTaskDetailsResponseSchema, detailsResp) as any;
				const logsResp = await rpc('ListLogs', () =>
					client.listLogs({ taskId: BigInt(taskId) }),
				);
				const logsJson = toJson(ListLogsResponseSchema, logsResp) as any;
				returnData.push({
					json: { ...detailsJson, logs: logsJson.entries || [] },
					pairedItem: { item: i },
				});
			} else if (operation === 'update') {
				const resp = await rpc('UpdateTask', () =>
					client.updateTask({
						id: BigInt(this.getNodeParameter('taskId', i) as number),
						addInstructions: [
							{ text: this.getNodeParameter('updateInstruction', i) as string },
						],
						start: this.getNodeParameter('start', i) as boolean,
					}),
				);
				const json = toJson(UpdateTaskResponseSchema, resp) as any;
				returnData.push({ json, pairedItem: { item: i } });
			} else if (operation === 'cancel') {
				const taskId = this.getNodeParameter('taskId', i) as number;
				const resp = await rpc('CancelTask', () =>
					client.cancelTask({ id: BigInt(taskId) }),
				);
				const json = toJson(CancelTaskResponseSchema, resp) as any;
				returnData.push({ json, pairedItem: { item: i } });
			}
		}

		return [returnData];
	}
}
