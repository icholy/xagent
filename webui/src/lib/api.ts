const API_BASE = '/xagent.v1.XAgentService';

export async function rpc<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
  const resp = await fetch(`${API_BASE}/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  });
  if (!resp.ok) {
    throw new Error(await resp.text());
  }
  return resp.json();
}

// Types
export interface Instruction {
  text: string;
  url?: string;
}

export interface Task {
  id: string;
  name: string;
  workspace: string;
  status: TaskStatus;
  instructions: Instruction[];
  parent?: string;
  createdAt: string;
}

export type TaskStatus =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'restarting'
  | 'archived';

export interface Link {
  id: number;
  taskId: string;
  url: string;
  title?: string;
  relevance?: string;
  notify: boolean;
}

export interface LogEntry {
  type: string;
  content: string;
  createdAt: string;
}

export interface Event {
  id: number;
  description: string;
  url?: string;
  data?: string;
  createdAt: string;
}

// API methods
export async function listTasks(): Promise<{ tasks: Task[] }> {
  return rpc('ListTasks');
}

export async function getTask(id: string): Promise<{ task: Task }> {
  return rpc('GetTask', { id });
}

export async function createTask(params: {
  name?: string;
  workspace: string;
  instructions: Instruction[];
}): Promise<{ task: Task }> {
  return rpc('CreateTask', params);
}

export async function updateTask(params: {
  id: string;
  name?: string;
  status?: string;
  addInstructions?: Instruction[];
}): Promise<{ task: Task }> {
  return rpc('UpdateTask', params);
}

export async function deleteTask(id: string): Promise<void> {
  return rpc('DeleteTask', { id });
}

export async function listChildTasks(parentId: string): Promise<{ tasks: Task[] }> {
  return rpc('ListChildTasks', { parentId });
}

export async function listLogs(taskId: string): Promise<{ entries: LogEntry[] }> {
  return rpc('ListLogs', { taskId });
}

export async function listLinks(taskId: string): Promise<{ links: Link[] }> {
  return rpc('ListLinks', { taskId });
}

export async function listEvents(): Promise<{ events: Event[] }> {
  return rpc('ListEvents');
}

export async function getEvent(id: number): Promise<{ event: Event }> {
  return rpc('GetEvent', { id });
}

export async function createEvent(params: {
  description: string;
  url?: string;
  data?: string;
}): Promise<{ event: Event }> {
  return rpc('CreateEvent', params);
}

export async function deleteEvent(id: number): Promise<void> {
  return rpc('DeleteEvent', { id });
}

export async function processEvent(id: number): Promise<void> {
  return rpc('ProcessEvent', { id });
}

export async function addEventTask(eventId: number, taskId: string): Promise<void> {
  return rpc('AddEventTask', { eventId, taskId });
}

export async function removeEventTask(eventId: number, taskId: string): Promise<void> {
  return rpc('RemoveEventTask', { eventId, taskId });
}

export async function listEventTasks(eventId: number): Promise<{ tasks: Task[] }> {
  return rpc('ListEventTasks', { eventId });
}

export async function listEventsByTask(taskId: string): Promise<{ events: Event[] }> {
  return rpc('ListEventsByTask', { taskId });
}
