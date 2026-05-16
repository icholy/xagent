import {
	IDataObject,
	IExecuteFunctions,
	INodeExecutionData,
	NodeApiError,
	NodeOperationError,
} from 'n8n-workflow';
import { createClient, ConnectError, type Client, type Interceptor } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { toJson, type DescMessage, type MessageShape } from '@bufbuild/protobuf';
import {
	XAgentService,
	CreateTaskResponseSchema,
	GetTaskDetailsResponseSchema,
	ListLogsResponseSchema,
	UpdateTaskResponseSchema,
	CancelTaskResponseSchema,
	TaskStatus,
} from '../../gen/xagent/v1/xagent_pb';

interface XAgentApiCredentials {
	serverUrl: string;
	apiKey: string;
}

const TERMINAL_STATUSES: TaskStatus[] = [
	TaskStatus.COMPLETED,
	TaskStatus.FAILED,
	TaskStatus.CANCELLED,
];

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
			const operation = this.getStringParameter('operation', i);
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

	private getStringParameter(name: string, i: number): string {
		const v = this.ctx.getNodeParameter(name, i);
		if (typeof v !== 'string') {
			throw new NodeOperationError(
				this.ctx.getNode(),
				`Parameter "${name}" must be a string, got ${typeof v}`,
				{ itemIndex: i },
			);
		}
		return v;
	}

	private getNumberParameter(name: string, i: number): number {
		const v = this.ctx.getNodeParameter(name, i);
		if (typeof v !== 'number') {
			throw new NodeOperationError(
				this.ctx.getNode(),
				`Parameter "${name}" must be a number, got ${typeof v}`,
				{ itemIndex: i },
			);
		}
		return v;
	}

	private getBooleanParameter(name: string, i: number): boolean {
		const v = this.ctx.getNodeParameter(name, i);
		if (typeof v !== 'boolean') {
			throw new NodeOperationError(
				this.ctx.getNode(),
				`Parameter "${name}" must be a boolean, got ${typeof v}`,
				{ itemIndex: i },
			);
		}
		return v;
	}

	private toJson<Desc extends DescMessage>(schema: Desc, msg: MessageShape<Desc>): IDataObject {
		return toJson(schema, msg) as IDataObject;
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
		const runner = this.getStringParameter('runner', i);
		const workspace = this.getStringParameter('workspace', i);
		const instruction = this.getStringParameter('instruction', i);
		const taskName = this.getStringParameter('taskName', i);

		const createResp = await this.rpc('CreateTask', () =>
			this.client.createTask({
				runner,
				workspace,
				instructions: [{ text: instruction }],
				name: taskName || undefined,
			}),
		);

		const waitForCompletion = this.getBooleanParameter('waitForCompletion', i);
		if (!waitForCompletion) {
			return {
				json: this.toJson(CreateTaskResponseSchema, createResp),
				pairedItem: { item: i },
			};
		}

		const taskId = createResp.task!.id;
		const pollInterval = this.getNumberParameter('pollInterval', i);
		const timeout = this.getNumberParameter('timeout', i);

		await this.waitFor(taskId, pollInterval, timeout, i);

		const detailsResp = await this.rpc('GetTaskDetails', () =>
			this.client.getTaskDetails({ id: taskId }),
		);
		if (detailsResp.task!.status === TaskStatus.FAILED) {
			throw new NodeOperationError(
				this.ctx.getNode(),
				`Task ${taskId} ended with status FAILED`,
				{ itemIndex: i },
			);
		}
		const logsResp = await this.rpc('ListLogs', () =>
			this.client.listLogs({ taskId }),
		);
		return {
			json: {
				...this.toJson(GetTaskDetailsResponseSchema, detailsResp),
				logs: this.toJson(ListLogsResponseSchema, logsResp).entries ?? [],
			},
			pairedItem: { item: i },
		};
	}

	private async waitFor(
		taskId: bigint,
		pollInterval: number,
		timeout: number,
		i: number,
	): Promise<void> {
		const startTime = Date.now();
		while (true) {
			await new Promise((resolve) => setTimeout(resolve, pollInterval * 1000));

			if (timeout > 0 && Date.now() - startTime > timeout * 1000) {
				throw new NodeOperationError(
					this.ctx.getNode(),
					`Task ${taskId} did not complete within ${timeout}s`,
					{ itemIndex: i },
				);
			}

			const resp = await this.rpc('GetTask', () =>
				this.client.getTask({ id: taskId }),
			);
			if (TERMINAL_STATUSES.includes(resp.task!.status)) {
				return;
			}
		}
	}

	private async getDetails(i: number): Promise<INodeExecutionData> {
		const taskId = BigInt(this.getNumberParameter('taskId', i));
		const detailsResp = await this.rpc('GetTaskDetails', () =>
			this.client.getTaskDetails({ id: taskId }),
		);
		const logsResp = await this.rpc('ListLogs', () =>
			this.client.listLogs({ taskId }),
		);
		return {
			json: {
				...this.toJson(GetTaskDetailsResponseSchema, detailsResp),
				logs: this.toJson(ListLogsResponseSchema, logsResp).entries ?? [],
			},
			pairedItem: { item: i },
		};
	}

	private async update(i: number): Promise<INodeExecutionData> {
		const resp = await this.rpc('UpdateTask', () =>
			this.client.updateTask({
				id: BigInt(this.getNumberParameter('taskId', i)),
				addInstructions: [{ text: this.getStringParameter('updateInstruction', i) }],
				start: this.getBooleanParameter('start', i),
			}),
		);
		return {
			json: this.toJson(UpdateTaskResponseSchema, resp),
			pairedItem: { item: i },
		};
	}

	private async cancel(i: number): Promise<INodeExecutionData> {
		const taskId = BigInt(this.getNumberParameter('taskId', i));
		const resp = await this.rpc('CancelTask', () =>
			this.client.cancelTask({ id: taskId }),
		);
		return {
			json: this.toJson(CancelTaskResponseSchema, resp),
			pairedItem: { item: i },
		};
	}
}
