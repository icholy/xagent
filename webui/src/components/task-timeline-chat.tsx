import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { TimelineItem } from '@/lib/timeline'
import { TaskTimeline } from '@/components/task-timeline'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { ChevronsDown, ChevronsUp, Loader2 } from 'lucide-react'

// A chat-style task timeline — the message list is an infinite scroll (older
// pages load as you scroll up, the tail follows new events) and the composer is
// locked to the bottom. Wraps the existing TaskTimeline renderer; the
// scroll/stick/prepend behavior lives here.
export function TaskTimelineChat({
  items,
  hasOlder,
  loadOlder,
  isLoadingOlder,
  composer,
}: {
  items: TimelineItem[]
  hasOlder: boolean
  loadOlder: () => void
  isLoadingOlder: boolean
  composer?: React.ReactNode
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const topSentinelRef = useRef<HTMLDivElement>(null)
  // Whether the user is pinned to the bottom; new tail items auto-scroll only then.
  const atBottomRef = useRef(true)
  // When an older page is prepended, remember the pre-fetch scroll height so we
  // can restore the viewport (otherwise the list jumps to the top).
  const prevScrollHeightRef = useRef<number | null>(null)
  const prevLenRef = useRef(items.length)
  // Drive the top scroll-shadow and the jump-button opacity (a button fades when
  // it's a no-op — you're already at that edge).
  const [scrolledFromTop, setScrolledFromTop] = useState(false)
  const [atBottom, setAtBottom] = useState(true)

  const syncScrollState = () => {
    const el = scrollRef.current
    if (!el) return
    const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    atBottomRef.current = bottom
    setAtBottom(bottom)
    setScrolledFromTop(el.scrollTop > 0)
  }

  // Open at the bottom (newest), like a chat.
  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
    syncScrollState()
  }, [])

  // On each item change: restore position after an older-page prepend, or follow
  // the tail when new items arrive and the user is already at the bottom.
  useLayoutEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const grew = items.length - prevLenRef.current
    prevLenRef.current = items.length
    if (prevScrollHeightRef.current !== null) {
      el.scrollTop += el.scrollHeight - prevScrollHeightRef.current
      prevScrollHeightRef.current = null
    } else if (grew > 0 && atBottomRef.current) {
      el.scrollTop = el.scrollHeight
    }
    syncScrollState()
  }, [items])

  // Infinite scroll up: pull older pages when the top sentinel enters view.
  useEffect(() => {
    const sentinel = topSentinelRef.current
    const el = scrollRef.current
    if (!sentinel || !el) return
    const obs = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasOlder && !isLoadingOlder) {
          prevScrollHeightRef.current = el.scrollHeight
          loadOlder()
        }
      },
      { root: el, threshold: 0 },
    )
    obs.observe(sentinel)
    return () => obs.disconnect()
  }, [hasOlder, isLoadingOlder, loadOlder])

  const jumpToTop = () => scrollRef.current?.scrollTo({ top: 0, behavior: 'smooth' })
  const jumpToBottom = () => {
    const el = scrollRef.current
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="relative min-h-0 flex-1">
        {/* Top-edge scroll shadow: fades in once content is scrolled above the fold. */}
        <div
          className={cn(
            'pointer-events-none absolute inset-x-0 top-0 z-10 h-4 bg-gradient-to-b from-black/10 to-transparent transition-opacity',
            scrolledFromTop ? 'opacity-100' : 'opacity-0',
          )}
        />
        <div
          ref={scrollRef}
          onScroll={syncScrollState}
          className="absolute inset-0 overflow-y-auto p-6"
        >
          {/* the timeline reads as a centered column, like a chat thread */}
          <div className="mx-auto w-full max-w-[860px]">
            <div ref={topSentinelRef} className="h-px" />
            {isLoadingOlder && (
              <div className="mb-4 flex justify-center text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
              </div>
            )}
            <TaskTimeline items={items} />
          </div>
        </div>

        {/* Jump-to-edge buttons overlay the scroll area's top-right / bottom-right
            corners, each rendered only when there's somewhere to jump. */}
        {scrolledFromTop && (
          <Button
            variant="secondary"
            size="icon"
            className="absolute right-3 top-3 z-20 rounded-full opacity-70 shadow transition-opacity hover:opacity-100"
            onClick={jumpToTop}
            title="Jump to top"
            aria-label="Jump to top"
          >
            <ChevronsUp className="h-4 w-4" />
          </Button>
        )}
        {!atBottom && (
          <Button
            variant="secondary"
            size="icon"
            className="absolute bottom-3 right-3 z-20 rounded-full opacity-70 shadow transition-opacity hover:opacity-100"
            onClick={jumpToBottom}
            title="Jump to bottom"
            aria-label="Jump to bottom"
          >
            <ChevronsDown className="h-4 w-4" />
          </Button>
        )}
      </div>
      {composer && (
        <div className="shrink-0 border-t p-4">
          <div className="mx-auto w-full max-w-[860px]">{composer}</div>
        </div>
      )}
    </div>
  )
}
