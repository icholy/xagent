import { ICredentialTestRequest, ICredentialType, INodeProperties } from 'n8n-workflow';

export class XAgentApi implements ICredentialType {
	name = 'XAgentApi';
	displayName = 'XAgent API';
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

	test: ICredentialTestRequest = {
		request: {
			baseURL: '={{$credentials.serverUrl}}',
			url: '/xagent.v1.XAgentService/Ping',
			method: 'POST',
			headers: {
				'Content-Type': 'application/json',
				Authorization: '=Bearer {{$credentials.apiKey}}',
				'X-Auth-Type': 'key',
				'Connect-Protocol-Version': '1',
			},
			body: {},
			json: true,
		},
	};
}
