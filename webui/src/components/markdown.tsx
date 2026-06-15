import { useState } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

// Configured once at module scope so the plugin arrays keep a stable identity
// across renders (react-markdown memoizes on them).
const remarkPlugins = [remarkGfm]
const rehypePlugins = [rehypeHighlight]

// Links in rendered markdown should open in a new tab rather than navigating
// away from the SPA.
const components: Components = {
  a: ({ children, ...props }) => (
    <a {...props} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  ),
}

// Markdown renders task instructions, agent output and event bodies with GFM
// (tables, task lists, strikethrough, autolinks) plus code-block syntax
// highlighting. Styling comes from the Tailwind typography (`prose`) plugin;
// highlight.js token colors live in index.css.
export function Markdown({ text, className }: { text: string; className?: string }) {
  return (
    <div
      className={cn(
        'prose prose-sm dark:prose-invert max-w-none break-words text-foreground',
        className,
      )}
    >
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        rehypePlugins={rehypePlugins}
        components={components}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}

// CollapsibleMarkdown collapses tall content (e.g. verbose agent output) behind
// a "Show more" toggle.
export function CollapsibleMarkdown({ text }: { text: string }) {
  const long = text.length > 320 || text.split('\n').length > 4
  const [open, setOpen] = useState(false)
  if (!long) return <Markdown text={text} />
  return (
    <div>
      <div className={cn(!open && 'relative max-h-24 overflow-hidden')}>
        <Markdown text={text} />
        {!open && (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-gradient-to-t from-card to-transparent" />
        )}
      </div>
      <Button
        variant="ghost"
        size="sm"
        className="mt-1 h-6 px-2 text-xs text-muted-foreground"
        onClick={() => setOpen((o) => !o)}
      >
        {open ? 'Show less' : 'Show more'}
      </Button>
    </div>
  )
}
