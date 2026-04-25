import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createConnectTransport } from '@connectrpc/connect-web'
import { routeTree } from './routeTree.gen'
import { AuthTransport } from './lib/transport'
import { NotificationWebSocket } from './lib/notification-websocket'
import { ServicesProvider } from './lib/services'
import './index.css'

const auth = new AuthTransport()
const transport = createConnectTransport({ baseUrl: '/', fetch: auth.fetch })
const ws = new NotificationWebSocket()

auth.onOrgChange(() => ws.reconnect())

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
      <ServicesProvider services={{ auth, ws }}>
        <TransportProvider transport={transport}>
          <QueryClientProvider client={queryClient}>
            <RouterProvider router={router} />
          </QueryClientProvider>
        </TransportProvider>
      </ServicesProvider>
    </StrictMode>
  )
}
