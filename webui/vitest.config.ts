import path from 'path'
import { defineConfig } from 'vitest/config'

// Standalone Vitest config so the test runner doesn't pull in the full Vite
// build pipeline (Tailwind, the TanStack Router plugin, etc). Tests start out
// as plain Node-environment unit tests; switch `environment` to 'jsdom' and add
// a setup file once component/hook tests need a DOM.
export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
