import { createFileRoute, redirect } from '@tanstack/react-router'

/**
 * /dashboard used to be the API-keys screen with a usage summary bolted on.
 * The Modernist design splits those into /usage and /keys, so this stays
 * as a redirect to keep older links and bookmarks working.
 */
export const Route = createFileRoute('/dashboard')({
  beforeLoad: () => {
    throw redirect({ to: '/usage' })
  },
})
