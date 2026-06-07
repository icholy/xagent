import path from 'path'
import { defineConfig } from 'vitest/config'

// Standalone Vitest config so the test runner doesn't pull in the full Vite
// build pipeline (Tailwind, the TanStack Router plugin, etc). The default
// environment is plain Node; tests that need a DOM opt in per-file with a
// `// @vitest-environment happy-dom` docblock (see transport.test.ts).
export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.{ts,tsx}'],
    // Node 22+ ships a built-in localStorage global that is inert unless
    // --localstorage-file is set, and it shadows the one happy-dom installs.
    // Disable it so DOM-environment tests get happy-dom's working Storage.
    pool: 'forks',
    execArgv: ['--no-experimental-webstorage'],
  },
})
