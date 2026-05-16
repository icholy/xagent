import {
	IExecuteFunctions,
	INodeExecutionData,
	NodeApiError,
	NodeOperationError,
} from 'n8n-workflow';
import { createClient, ConnectError, type Client, type Interceptor } from '@connectrpc/connect';
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

interface XAgentApiCredentials {
	serverUrl: string;
	apiKey: string;
}

const TERMINAL_STATUSES = ['COMPLETED', 'FAILED', 'CANCELLED'];

export class XAgentExecutor {
	private ctx: IExecuteFunctions;
	private client!: Client<typeof XAgentService>;

	constructor(ctx: IExecuteFunctions) {
		this.ctx = ctx;
	}

	async execute(): Promise<INodeExecutionData[][]> {
		await this.buildClient();
		const items = this.ctx.getInputData();
		const returnData: INodeExecutionData[] = [];
		for (let i = 0; i < items.length; i++) {
			const operation = this.ctx.getNodeParameter('operation', i) as string;
			switch (operation) {
				case 'create':
					returnData.push(await this.create(i));
					break;
				case 'getDetails':
					returnData.push(await this.getDetails(i));
					break;
				case 'update':
					returnData.push(await this.update(i));
					break;
				case 'cancel':
					returnData.push(await this.cancel(i));
					break;
			}
		}
		return [returnData];
	}

	private async buildClient(): Promise<void> {
		const credentials = await this.ctx.getCredentials<XAgentApiCredentials>('XAgentApi');
		const serverUrl = credentials.serverUrl.replace(/\/$/, '');
		const apiKey = credentials.apiKey;
		const authInterceptor: Interceptor = (next) => async (req) => {
			req.header.set('Authorization', `Bearer ${apiKey}`);
			req.header.set('X-Auth-Type', 'key');
			return next(req);
		};
		const transport = createConnectTransport({
			baseUrl: serverUrl,
			interceptors: [authInterceptor],
		});
		this.client = createClient(XAgentService, transport);
	}

	private async rpc<T>(method: string, fn: () => Promise<T>): Promise<T> {
		try {
			return await fn();
		} catch (err) {
			if (err instanceof ConnectError) {
				throw new NodeApiError(this.ctx.getNode(), {}, {
					message: `${method}: ${err.message}`,
				});
			}
			throw err;
		}
	}

	private async create(i: number): Promise<INodeExecutionData> {
		const runner = this.ctx.getNodeParameter('runner', i) as string;
		const workspace = this.ctx.getNodeParameter('workspace', i) as string;
		const instruction = this.ctx.getNodeParameter('instruction', i) as string;
		const taskName = this.ctx.getNodeParameter('taskName', i) as string;

		const createResp = await this.rpc('CreateTask', () =>
			this.client.createTask({
				runner,
				workspace,
				instructions: [{ text: instruction }],
				name: taskName || undefined,
			}),
		);

		const waitForCompletion = this.ctx.getNodeParameter('waitForCompletion', i) as boolean;
		if (!waitForCompletion) {
			return {
				json: toJson(CreateTaskResponseSchema, createResp) as any,
				pairedItem: { item: i },
			};
		}

		const taskId = createResp.task!.id;
		const pollInterval = this.ctx.getNodeParameter('pollInterval', i) as number;
		const timeout = this.ctx.getNodeParameter('timeout', i) as number;
		const startTime = Date.now();

		let detailsJson: any;
		while (true) {
			await new Promise((resolve) => setTimeout(resolve, pollInterval * 1000));

			if (timeout > 0 && Date.now() - startTime > timeout * 1000) {
				throw new NodeOperationError(
					this.ctx.getNode(),
					`Task ${taskId} did not complete within ${timeout}s`,
					{ itemIndex: i },
				);
			}

			const detailsResp = await this.rpc('GetTaskDetails', () =>
				this.client.getTaskDetails({ id: taskId }),
			);
			detailsJson = toJson(GetTaskDetailsResponseSchema, detailsResp) as any;
			if (TERMINAL_STATUSES.includes(detailsJson.task.status)) {
				break;
			}
		}

		const logsResp = await this.rpc('ListLogs', () =>
			this.client.listLogs({ taskId }),
		);
		const logsJson = toJson(ListLogsResponseSchema, logsResp) as any;
		if (detailsJson.task.status === 'FAILED') {
			throw new NodeOperationError(
				this.ctx.getNode(),
				`Task ${taskId} ended with status FAILED`,
				{ itemIndex: i },
			);
		}
		return {
			json: { ...detailsJson, logs: logsJson.entries || [] },
			pairedItem: { item: i },
		};
	}

	private async getDetails(i: number): Promise<INodeExecutionData> {
		const taskId = BigInt(this.ctx.getNodeParameter('taskId', i) as number);
		const detailsResp = await this.rpc('GetTaskDetails', () =>
			this.client.getTaskDetails({ id: taskId }),
		);
		const detailsJson = toJson(GetTaskDetailsResponseSchema, detailsResp) as any;
		const logsResp = await this.rpc('ListLogs', () =>
			this.client.listLogs({ taskId }),
		);
		const logsJson = toJson(ListLogsResponseSchema, logsResp) as any;
		return {
			json: { ...detailsJson, logs: logsJson.entries || [] },
			pairedItem: { item: i },
		};
	}

	private async update(i: number): Promise<INodeExecutionData> {
		const resp = await this.rpc('UpdateTask', () =>
			this.client.updateTask({
				id: BigInt(this.ctx.getNodeParameter('taskId', i) as number),
				addInstructions: [
					{ text: this.ctx.getNodeParameter('updateInstruction', i) as string },
				],
				start: this.ctx.getNodeParameter('start', i) as boolean,
			}),
		);
		return {
			json: toJson(UpdateTaskResponseSchema, resp) as any,
			pairedItem: { item: i },
		};
	}

	private async cancel(i: number): Promise<INodeExecutionData> {
		const taskId = BigInt(this.ctx.getNodeParameter('taskId', i) as number);
		const resp = await this.rpc('CancelTask', () =>
			this.client.cancelTask({ id: taskId }),
		);
		return {
			json: toJson(CancelTaskResponseSchema, resp) as any,
			pairedItem: { item: i },
		};
	}
}
