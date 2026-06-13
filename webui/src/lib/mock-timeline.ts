// Mock data for experimenting with the unified task timeline (issues #918 /
// #947). This is throwaway UI-only data — no backend involvement.
//
// The stream below is reconstructed from a real task (#827, "Fix nvim
// LspTypescriptGoToSourceDefinition command") and intentionally contains ONLY
// real data: its instruction, the audit/info/mcp/llm log entries, the links,
// and the two issue #8 comments that woke it. Nothing is invented — there are
// no get_my_task calls, test output, or fabricated event bodies. The event
// bodies are the verbatim comments icholy posted on the issue; the two restarts
// have no agent output because the task itself logged none (the agent replied
// on GitHub, which the task record doesn't capture).

export type TimelineDirection = 'to-agent' | 'from-agent' | 'about-task'

export type ReportLevel = 'info' | 'mcp' | 'audit' | 'error'

export type LifecycleKind =
  | 'pending'
  | 'started'
  | 'restarted'
  | 'completed'
  | 'failed'
  | 'cancelled'

export type ExternalSource = 'github' | 'jira'

// A single entry in the unified task stream. The discriminated `kind` decides
// the visual treatment; `direction` is informational (to-agent / from-agent /
// about-task) and could drive alignment or grouping later.
export type TimelineItem =
  | {
      kind: 'instruction'
      id: string
      at: Date
      direction: 'to-agent'
      text: string
      url?: string
      wakes?: boolean
    }
  | {
      kind: 'agent'
      id: string
      at: Date
      direction: 'from-agent'
      text: string
    }
  | {
      kind: 'external'
      id: string
      at: Date
      direction: 'to-agent'
      source: ExternalSource
      description: string
      data?: string
      url?: string
      wakes?: boolean
    }
  | {
      kind: 'lifecycle'
      id: string
      at: Date
      direction: 'about-task'
      event: LifecycleKind
      detail?: string
    }
  | {
      kind: 'link'
      id: string
      at: Date
      direction: 'from-agent'
      title: string
      url: string
      source?: ExternalSource
      relevance?: string
      subscribed?: boolean
    }
  | {
      kind: 'report'
      id: string
      at: Date
      direction: 'from-agent'
      level: ReportLevel
      content: string
    }

// Build timestamps relative to "now" so the relative-time labels stay fresh.
// `m` is minutes ago. The values only encode ordering — the task's real logs
// and links carry no timestamps.
const now = Date.now()
const ago = (m: number) => new Date(now - m * 60_000)

const ISSUE_8 = 'https://github.com/icholy/dotfiles/issues/8'
const PR_9 = 'https://github.com/icholy/dotfiles/pull/9'
const PR_10 = 'https://github.com/icholy/dotfiles/pull/10'
const PR_11 = 'https://github.com/icholy/dotfiles/pull/11'

export const MOCK_TIMELINE: TimelineItem[] = [
  // ----- run 1: created by a routing rule on issue_assigned -----------------
  {
    kind: 'lifecycle',
    id: 'lc-created',
    at: ago(2880),
    direction: 'about-task',
    event: 'pending',
    detail: 'webhook created task',
  },
  {
    kind: 'link',
    id: 'link-issue-8',
    at: ago(2880),
    direction: 'from-agent',
    source: 'github',
    title: 'icholy assigned issue #8 to @icholy-bot',
    url: ISSUE_8,
    relevance: 'trigger',
    subscribed: true,
  },
  {
    kind: 'instruction',
    id: 'inst-1',
    at: ago(2879),
    direction: 'to-agent',
    text: 'You were created by a routing rule in response to a github issue_assigned event.',
  },
  {
    kind: 'lifecycle',
    id: 'lc-start-1',
    at: ago(2878),
    direction: 'about-task',
    event: 'started',
    detail: 'container started',
  },
  {
    kind: 'report',
    id: 'rep-name',
    at: ago(2876),
    direction: 'from-agent',
    level: 'audit',
    content: 'updated task: name',
  },
  {
    kind: 'link',
    id: 'link-pr-9',
    at: ago(2875),
    direction: 'from-agent',
    source: 'github',
    title: 'PR #9: restore gs go-to-source-definition mapping',
    url: PR_9,
    relevance: 'PR fixing issue #8 (broken gs / LspTypescriptGoToSourceDefinition mapping)',
    subscribed: true,
  },
  {
    kind: 'agent',
    id: 'agent-diagnosis',
    at: ago(2874),
    direction: 'from-agent',
    text: 'Diagnosed issue #8: the `gs` keymap in nvim init.lua called `:LspTypescriptGoToSourceDefinition`, a command provided by a TS LSP plugin (typescript-tools.nvim) no longer used. Commit f6c8e78 replaced a working inline Lua implementation with that nonexistent command. Restored the Lua implementation (invokes `_typescript.goToSourceDefinition` workspace command). Opened PR #9.',
  },
  {
    kind: 'lifecycle',
    id: 'lc-done-1',
    at: ago(2873),
    direction: 'about-task',
    event: 'completed',
    detail: 'container exited successfully',
  },

  // ----- icholy's comment wakes the task (run 2 trigger) -------------------
  {
    kind: 'external',
    id: 'ext-deprecation',
    at: ago(1442),
    direction: 'to-agent',
    source: 'github',
    wakes: true,
    description: 'icholy commented on issue #8',
    data: '@icholy-bot I merged that PR and it\'s working, but I\'m getting this logged\n\n> client.request is deprecated. Run ":checkhealth vim.deprecated" for more information',
    url: `${ISSUE_8}#issuecomment-4670840200`,
  },

  // ----- run 2: woken by webhook; produced PR #10 --------------------------
  {
    kind: 'lifecycle',
    id: 'lc-restart-2',
    at: ago(1440),
    direction: 'about-task',
    event: 'restarted',
    detail: 'container restarted',
  },
  {
    kind: 'link',
    id: 'link-pr-10',
    at: ago(1437),
    direction: 'from-agent',
    source: 'github',
    title: 'PR #10: silence gs deprecation warnings',
    url: PR_10,
    relevance: 'Follow-up PR fixing the client.request deprecation warning reported on issue #8',
    subscribed: true,
  },
  {
    kind: 'lifecycle',
    id: 'lc-done-2',
    at: ago(1436),
    direction: 'about-task',
    event: 'completed',
    detail: 'container exited successfully',
  },

  // ----- icholy's follow-up comment wakes the task (run 3 trigger) ----------
  {
    kind: 'external',
    id: 'ext-rootcause',
    at: ago(182),
    direction: 'to-agent',
    source: 'github',
    wakes: true,
    description: 'icholy commented on issue #8',
    data: '@icholy-bot this is working, but I just check lsp-config and the function is still there https://github.com/neovim/nvim-lspconfig/blob/master/lsp/ts_ls.lua#L179-L198',
    url: `${ISSUE_8}#issuecomment-4670917365`,
  },

  // ----- run 3: woken by webhook; produced PR #11 --------------------------
  {
    kind: 'lifecycle',
    id: 'lc-restart-3',
    at: ago(180),
    direction: 'about-task',
    event: 'restarted',
    detail: 'container restarted',
  },
  {
    kind: 'link',
    id: 'link-pr-11',
    at: ago(177),
    direction: 'from-agent',
    source: 'github',
    title: 'PR #11: use built-in LspTypescriptGoToSourceDefinition',
    url: PR_11,
    relevance:
      "PR switching gs to lspconfig's built-in command; addresses root cause (on_attach clobbering) raised on issue #8",
    subscribed: true,
  },
  {
    kind: 'lifecycle',
    id: 'lc-done-3',
    at: ago(176),
    direction: 'about-task',
    event: 'completed',
    detail: 'container exited successfully',
  },
]
