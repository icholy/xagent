import * as React from 'react'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

interface TruncatedTextProps {
  text: string
  maxLength?: number
  className?: string
  as?: React.ElementType
  asProps?: Record<string, unknown>
}

export function TruncatedText({
  text,
  maxLength = 100,
  className,
  as: Component,
  asProps,
}: TruncatedTextProps) {
  const isTruncated = text.length > maxLength
  const truncatedText = isTruncated ? text.slice(0, maxLength) + '...' : text

  const content = Component ? (
    <Component {...asProps}>{truncatedText}</Component>
  ) : (
    truncatedText
  )

  if (!isTruncated) {
    return <span className={className}>{content}</span>
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn('cursor-help', className)}>{content}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-md whitespace-pre-wrap break-words">
        {text}
      </TooltipContent>
    </Tooltip>
  )
}
