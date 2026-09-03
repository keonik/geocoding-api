import { Link, useNavigate } from '@tanstack/react-router'
import { authAPI } from '@/api/auth'
import { ThemeToggle } from '@/components/theme-toggle'

/**
 * The authed app's top bar. Modernist `.nav`: a 2px rule under a single row,
 * accent on the current item, no elevation.
 *
 * Billing is deliberately absent — there is no invoice or subscription
 * endpoint behind it yet. Docs points at the server-rendered docs/ pages
 * rather than a route.
 */
export function AppNav() {
  const navigate = useNavigate()

  let email = ''
  try {
    email = (JSON.parse(localStorage.getItem('user') || '{}') as { email?: string }).email || ''
  } catch {
    // A corrupt `user` blob should not take the nav down with it.
  }

  const signOut = () => {
    authAPI.logout()
    navigate({ to: '/' })
  }

  return (
    <div className="nav flex-wrap gap-[14px] px-[clamp(16px,4vw,40px)] py-3">
      <Link to="/" className="nav-brand">
        GeoCode API
      </Link>

      <Link to="/usage" activeProps={{ 'aria-current': 'page' }}>
        Usage
      </Link>
      <Link to="/api-keys" activeProps={{ 'aria-current': 'page' }}>
        API keys
      </Link>
      <Link to="/playground" activeProps={{ 'aria-current': 'page' }}>
        Playground
      </Link>
      <a href="/docs" target="_blank" rel="noopener noreferrer">
        Docs
      </a>

      {email && (
        <span className="font-mono text-xs opacity-65">{email}</span>
      )}
      <ThemeToggle />
      <button type="button" className="btn btn-secondary" onClick={signOut}>
        Sign out
      </button>
    </div>
  )
}
