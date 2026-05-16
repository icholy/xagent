import {
	IExecuteFunctions,
	INodeExecutionData,
	INodeType,
	INodeTypeDescription,
} from 'n8n-workflow';
import { XAgentExecutor } from './XAgentExecutor';

export class XAgent implements INodeType {
	description: INodeTypeDescription = {
		displayName: 'XAgent',
		name: 'xagent',
		icon: 'file:xagent.png',
		group: ['transform'],
		version: 1,
		subtitle: '={{$parameter["operation"]}}',
		description: 'Create and run xagent tasks',
		defaults: { name: 'xagent' },
		inputs: ['main'],
		outputs: ['main'],
		credentials: [{ name: 'XAgentApi', required: true }],
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
		return new XAgentExecutor(this).execute();
	}
}
