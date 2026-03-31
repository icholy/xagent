---
name: proposal
description: Create a design proposal for a feature or change. Use when the user wants to plan or design something before implementing it.
---

# Creating Design Proposals

GitHub issues describe problems. Proposals describe solutions. This skill turns an issue into a proposal markdown file in a PR so the design can be iterated on via code review.

## Workflow

1. **Issue**: If a GitHub issue was provided in the prompt, read it. If no issue was provided, create one that describes only the problem, not the solution.
2. **Understand the codebase**: Research the relevant parts of the codebase to inform the design. Read existing code, schemas, types, and patterns that the proposal will build on.
3. **Write the proposal**: Create a markdown file in `proposals/` named after the feature (e.g., `proposals/server-managed-workspaces.md`).
4. **Create a PR**: Open a PR containing only the proposal markdown file, linking back to the issue.

## Proposal Format

```markdown
# Title

Status: accepted|rejected|pending
Issue: #NNN

## Problem

Brief summary of the problem from the issue.

## Design

The concrete design. Include:
- Database schema changes (SQL migrations)
- API changes (proto definitions)
- CLI changes
- Behavioral changes to existing components
- Key implementation details

Be specific. Use actual type names, table names, and code patterns from the codebase.

## Trade-offs

What alternatives were considered and why this approach was chosen.

## Open Questions

Unresolved decisions that need input.
```

## Guidelines

- The proposal file goes in the `proposals/` directory at the repo root.
- Use kebab-case for filenames: `proposals/my-feature.md`.
- Link to the issue in the proposal body.
- The PR title should be: `proposal: <short description>`.
- The PR body should reference the issue with `Closes #NNN` or `Related to #NNN` as appropriate.
- Keep the design grounded in the existing codebase. Reference actual types, tables, and patterns.
- Do not implement anything. The proposal is a document, not code.
