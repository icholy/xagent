import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Loader2 } from 'lucide-react'

export const ROUTING_SOURCES = ['github', 'atlassian'] as const

export interface RoutingRuleFormValues {
  source: string
  type: string
  prefix: string
  mention: string
}

export const emptyRoutingRule: RoutingRuleFormValues = {
  source: '',
  type: '',
  prefix: '',
  mention: '',
}

interface RoutingRuleFormProps {
  initialValues: RoutingRuleFormValues
  submitLabel: string
  isSubmitting?: boolean
  error?: string | null
  onSubmit: (values: RoutingRuleFormValues) => void | Promise<void>
  onCancel: () => void
}

export function RoutingRuleForm({
  initialValues,
  submitLabel,
  isSubmitting,
  error,
  onSubmit,
  onCancel,
}: RoutingRuleFormProps) {
  const [values, setValues] = useState<RoutingRuleFormValues>(initialValues)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!values.source) return
    void onSubmit(values)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <div className="space-y-2">
        <Label htmlFor="source">Source</Label>
        <Select
          value={values.source}
          onValueChange={(v) => setValues({ ...values, source: v })}
          required
        >
          <SelectTrigger id="source">
            <SelectValue placeholder="Select a source" />
          </SelectTrigger>
          <SelectContent>
            {ROUTING_SOURCES.map((s) => (
              <SelectItem key={s} value={s}>
                {s}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-2">
        <Label htmlFor="type">Type</Label>
        <Input
          id="type"
          placeholder="e.g. pull_request, issue_comment"
          value={values.type}
          onChange={(e) => setValues({ ...values, type: e.target.value })}
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="prefix">Prefix</Label>
        <Input
          id="prefix"
          placeholder="Message prefix to match"
          value={values.prefix}
          onChange={(e) => setValues({ ...values, prefix: e.target.value })}
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="mention">Mention</Label>
        <Input
          id="mention"
          placeholder="Mention to match (e.g. username)"
          value={values.mention}
          onChange={(e) => setValues({ ...values, mention: e.target.value })}
        />
      </div>

      {error && <div className="text-destructive text-sm">{error}</div>}

      <div className="flex gap-2">
        <Button type="submit" disabled={isSubmitting || !values.source}>
          {isSubmitting && <Loader2 className="h-4 w-4 animate-spin" />}
          {submitLabel}
        </Button>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </form>
  )
}
