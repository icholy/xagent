import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createConnectTransport } from '@connectrpc/connect-web'
import { routeTree } from './routeTree.gen'
import { AuthTransport } from './lib/transport'
import { NotificationSSE } from './lib/notification-sse'
import { ServicesProvider } from './lib/services'
import './index.css'

const auth = new AuthTransport()
const transport = createConnectTransport({ baseUrl: '/', fetch: auth.fetch })
const notifications = new NotificationSSE(auth.clientId)

notifications.setOrgId(auth.getOrgId())
auth.onOrgChange((orgId) => notifications.setOrgId(orgId))

const queryClient = new QueryClient()

const router = createRouter({
  routeTree,
  basepath: '/ui',
  context: { queryClient },
  defaultPreload: 'intent',
  defaultPreloadStaleTime: 0,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
  interface StaticDataRouteOption {
    orgSwitchRedirect?: string
  }
}

const rootElement = document.getElementById('root')!
if (!rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement)
  root.render(
    <StrictMode>
      <ServicesProvider services={{ auth, notifications }}>
        <TransportProvider transport={transport}>
          <QueryClientProvider client={queryClient}>
            <RouterProvider router={router} />
          </QueryClientProvider>
        </TransportProvider>
      </ServicesProvider>
    </StrictMode>
  )
}
