import { ICredentialType, INodeProperties } from 'n8n-workflow';

export class XagentApi implements ICredentialType {
	name = 'xagentApi';
	displayName = 'xagent API';
	documentationUrl = 'https://github.com/icholy/xagent';
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
