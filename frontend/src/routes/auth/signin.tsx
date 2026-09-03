import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { useState } from 'react'
import { authAPI } from '@/api/auth'

export const Route = createFileRoute('/auth/signin')({
  component: SignIn,
})

function SignIn() {
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      const response = await authAPI.login({ email, password })

      if (response.success && response.data) {
        localStorage.setItem('authToken', response.data.token)
        localStorage.setItem('user', JSON.stringify(response.data.user))
        navigate({ to: '/usage' })
      } else {
        setError(response.error || 'Login failed')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen flex-wrap gap-[2px] bg-[var(--color-divider)]">
      {/* Inverted panel — the only place the system uses chrome ink on chrome ground */}
      <div className="flex min-w-0 flex-[1_1_360px] flex-col gap-7 bg-[var(--gc-chrome-bg)] px-[clamp(20px,4vw,40px)] py-[clamp(32px,5vw,56px)] text-[var(--gc-chrome-ink)]">
        <Link to="/" className="mb-auto font-display text-lg font-extrabold text-[var(--gc-chrome-ink)] no-underline">
          GeoCode API
        </Link>
        <h2 className="m-0 max-w-[18ch] text-[clamp(28px,5vw,40px)] leading-[1.02] tracking-[-0.025em]">
          Coordinates for the addresses you already have.
        </h2>
        <div className="font-mono text-xs leading-loose opacity-70">
          <div>us zip + zcta · ohio parcel addresses</div>
          <div>rest · api key auth · no sdk</div>
          <div>metered per call</div>
        </div>
      </div>

      <form
        onSubmit={handleSubmit}
        className="flex min-w-0 flex-[1_1_320px] flex-col justify-center bg-[var(--color-bg)] px-[clamp(20px,4vw,40px)] py-[clamp(32px,5vw,56px)]"
      >
        <h3 className="m-0 mb-1.5 text-[28px]">Sign in</h3>
        <p className="m-0 mb-7 text-sm opacity-70">
          New here? <Link to="/auth/signup">Create an account</Link> — first key issued
          instantly.
        </p>

        {error && (
          <div
            role="alert"
            className="mb-4 border-l-2 border-[var(--color-accent)] bg-[var(--color-accent-100)] p-3 text-sm text-[var(--color-accent-800)]"
          >
            {error}
          </div>
        )}

        <div className="field mb-4">
          <label htmlFor="gc-email">Work email</label>
          <input
            className="input"
            id="gc-email"
            type="email"
            autoComplete="email"
            placeholder="you@company.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            disabled={loading}
          />
        </div>
        <div className="field mb-5">
          <label htmlFor="gc-pass">Password</label>
          <input
            className="input"
            id="gc-pass"
            type="password"
            autoComplete="current-password"
            placeholder="••••••••••"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            disabled={loading}
          />
        </div>

        <button type="submit" className="btn btn-primary btn-block px-4 py-3" disabled={loading}>
          {loading ? 'Signing in…' : 'Sign in'}
        </button>

        <div className="hr" />
        <div className="text-xs opacity-65">
          Keys are scoped per endpoint group and revocable at any time.
        </div>
      </form>
    </div>
  )
}
