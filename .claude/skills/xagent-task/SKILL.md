---
name: xagent-task
description: Create xagent tasks using the MCP tools. Use when the user wants to create a task for the xagent system.
---

# Creating xagent Tasks

Use the `mcp__xagent__create_task` and `mcp__xagent__list_workspaces` MCP tools to create tasks.

## Workflow

1. If the user hasn't specified a workspace, use `mcp__xagent__list_workspaces` to find the right one.
2. Create the task with `mcp__xagent__create_task`, always including the `runner` parameter.
3. Show the user the task URL from the response.

## Guidelines

- **Always set `runner`**: Use the runner_id from `list_workspaces` (don't omit it).
- **Name**: Keep task names short and descriptive (under 60 chars).
- **Instruction**: Write clear, actionable instructions. Include:
  - What needs to change and why
  - Relevant file paths or package names if known
  - Specific implementation hints if the user provided them
  - Details from the current conversation (code you've read, architecture discussed) so the agent doesn't have to rediscover it

## Container Environment

The agent runs inside a Docker container with a limited toolset. The available tools are defined in `mise.toml` at the repo root. Do **not** assume any CLI tools are available beyond what mise installs and standard Linux utilities. Do **not** assume any CLI tools are available beyond what mise installs and standard Linux utilities.
