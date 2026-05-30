import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { create } from '@bufbuild/protobuf'
import {
  getRoutingRules,
  setRoutingRules,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { RoutingRuleSchema } from '@/gen/xagent/v1/xagent_pb'
import { Card, CardContent } from '@/components/ui/card'
import { useOrgId } from '@/hooks/use-org-id'
import {
  RoutingRuleForm,
  emptyRoutingRule,
  type RoutingRuleFormValues,
} from '@/components/routing-rule-form'

export const Route = createFileRoute('/routing/new')({
  component: NewRoutingRulePage,
})

function NewRoutingRulePage() {
  const navigate = useNavigate()
  const orgId = useOrgId()
  const { data, isLoading } = useQuery(getRoutingRules, {})
  const mutation = useMutation(setRoutingRules)

  const handleSubmit = async (values: RoutingRuleFormValues) => {
    const rules = data?.rules ?? []
    const newRule = create(RoutingRuleSchema, values)
    await mutation.mutateAsync({ rules: [...rules, newRule] })
    navigate({ to: '/settings', search: { tab: 'events', org: orgId } })
  }

  const handleCancel = () => {
    navigate({ to: '/settings', search: { tab: 'events', org: orgId } })
  }

  return (
    <div className="container mx-auto py-8 px-4 space-y-6">
      <h1 className="text-2xl font-bold mb-6">New Routing Rule</h1>

      <Card>
        <CardContent className="pt-6">
          {isLoading ? (
            <div className="text-muted-foreground">Loading...</div>
          ) : (
            <RoutingRuleForm
              initialValues={emptyRoutingRule}
              submitLabel={mutation.isPending ? 'Saving...' : 'Create Rule'}
              isSubmitting={mutation.isPending}
              error={mutation.error?.message ?? null}
              onSubmit={handleSubmit}
              onCancel={handleCancel}
            />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
