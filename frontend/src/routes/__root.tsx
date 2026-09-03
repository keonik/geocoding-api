import { createRootRoute, Outlet } from '@tanstack/react-router'
import { Suspense, lazy } from 'react'
import { ThemeProvider } from '@/components/theme-provider'

/**
 * Devtools are dev-only. `@tanstack/router-devtools` is a devDependency, and
 * importing it at the top level pulled it into the production bundle as its
 * own chunk — the floating badge then rendered over the bottom-left of every
 * page for real users. Guarding a dynamic import on import.meta.env.DEV lets
 * Rollup drop the whole branch when it builds.
 */
const RouterDevtools = import.meta.env.DEV
  ? lazy(() =>
      import('@tanstack/router-devtools').then((m) => ({
        default: m.TanStackRouterDevtools,
      }))
    )
  : () => null

export const Route = createRootRoute({
  component: () => (
    <ThemeProvider defaultTheme="system" storageKey="geocoding-ui-theme">
      <Outlet />
      <Suspense fallback={null}>
        <RouterDevtools />
      </Suspense>
    </ThemeProvider>
  ),
})
