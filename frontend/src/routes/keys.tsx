import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useCallback, useEffect, useState } from 'react'
import { AppNav } from '@/components/app-nav'
import { apiKeysAPI } from '@/api/apiKeys'
import { usageAPI } from '@/api/usage'
import type { APIKey, UsageStats } from '@/types/api'

export const Route = createFileRoute('/keys')({
  component: APIKeysPage,
})

/* Mirrors the permission strings the create handler accepts. */
const SCOPES = [
  '*',
  'geocode',
  'search',
  'distance',
  'addresses',
  'counties',
  'cities',
  'states',
]

const num = (n: number) => n.toLocaleString('en-US')

function fmtDate(iso?: string) {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? '—'
    : d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

function APIKeysPage() {
  const navigate = useNavigate()
  const [keys, setKeys] = useState<APIKey[]>([])
  const [stats, setStats] = useState<UsageStats | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const [dialog, setDialog] = useState<'create' | 'created' | null>(null)
  const [newKeyName, setNewKeyName] = useState('')
  const [scopes, setScopes] = useState<string[]>(['geocode', 'search'])
  const [createdKey, setCreatedKey] = useState('')
  const [copied, setCopied] = useState(false)
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    try {
      // The limits panel is a nicety; a failure there must not hide the keys.
      const [k, s] = await Promise.allSettled([apiKeysAPI.list(), usageAPI.getStats()])
      if (k.status === 'fulfilled') setKeys(k.value.data?.api_keys ?? [])
      if (s.status === 'fulfilled') setStats(s.value.data ?? null)
      setError(
        k.status === 'rejected'
          ? k.reason instanceof Error
            ? k.reason.message
            : String(k.reason)
          : ''
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load API keys')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!localStorage.getItem('authToken')) {
      navigate({ to: '/auth/signin' })
      return
    }
    void load()
  }, [load, navigate])

  // Escape closes whichever dialog is open.
  useEffect(() => {
    if (!dialog) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setDialog(null)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [dialog])

  const toggleScope = (s: string) => {
    setScopes((prev) => {
      // '*' is exclusive: picking it clears the rest, picking any other clears it.
      if (s === '*') return prev.includes('*') ? [] : ['*']
      const without = prev.filter((x) => x !== '*')
      return without.includes(s) ? without.filter((x) => x !== s) : [...without, s]
    })
  }

  const createKey = async () => {
    setSaving(true)
    try {
      const res = await apiKeysAPI.create({
        name: newKeyName || 'untitled-key',
        permissions: scopes.length ? scopes : ['*'],
      })
      setCreatedKey(res.data?.key_string ?? '')
      setCopied(false)
      setDialog('created')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create key')
      setDialog(null)
    } finally {
      setSaving(false)
    }
  }

  const revoke = async (id: string) => {
    try {
      await apiKeysAPI.delete(id)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke key')
    }
  }

  const copyKey = async () => {
    try {
      await navigator.clipboard.writeText(createdKey)
      setCopied(true)
    } catch {
      // Clipboard is blocked outside a secure context; the key is on screen anyway.
      setCopied(false)
    }
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <AppNav />

      <div className="flex flex-wrap items-end justify-between gap-6 border-b-2 border-[var(--color-divider)] px-[clamp(20px,4vw,40px)] pt-8 pb-5">
        <div>
          <h2 className="m-0 mb-1 text-[clamp(26px,4vw,32px)]">API keys</h2>
          <div className="text-[13px] opacity-70">
            Keys are shown once at creation. Scope each key to the endpoints it needs.
          </div>
        </div>
        <button
          type="button"
          className="btn btn-primary px-[18px] py-[11px]"
          onClick={() => {
            setNewKeyName('')
            setScopes(['geocode', 'search'])
            setDialog('create')
          }}
        >
          Create key
        </button>
      </div>

      {error && (
        <div
          role="alert"
          className="mx-[clamp(20px,4vw,40px)] mt-4 border-l-2 border-[var(--color-accent)] bg-[var(--color-accent-100)] p-3 text-sm text-[var(--color-accent-800)]"
        >
          {error}
        </div>
      )}

      <div className="px-[clamp(20px,4vw,40px)] pt-6 pb-12">
        <div className="overflow-x-auto">
          <table className="table min-w-[720px]">
            <thead>
              <tr>
                <th>Name</th>
                <th>Key</th>
                <th>Scope</th>
                <th>Last used</th>
                <th>Created</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id}>
                  <td className="font-semibold">{k.name}</td>
                  <td className="font-mono text-[13px]">{k.key_preview}</td>
                  <td>
                    <span className="flex flex-wrap gap-1">
                      {k.permissions.map((p) => (
                        <span key={p} className="tag tag-accent">
                          {p === '*' ? 'all endpoints' : p}
                        </span>
                      ))}
                    </span>
                  </td>
                  <td className="opacity-75">{fmtDate(k.last_used_at)}</td>
                  <td className="opacity-75">{fmtDate(k.created_at)}</td>
                  <td className="text-right">
                    <button type="button" className="btn btn-ghost" onClick={() => revoke(k.id)}>
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
              {!loading && keys.length === 0 && (
                <tr>
                  <td colSpan={6} className="opacity-65">
                    No keys yet. Create one to start making calls.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <div className="mt-8 max-w-[640px] bg-[var(--color-surface)] p-5">
          <div className="mb-2 text-[11px] uppercase tracking-[0.1em] text-[var(--color-accent-700)]">
            Limits
          </div>
          <div className="text-sm leading-relaxed">
            {stats ? (
              <>
                Your {stats.usage_summary.month} allowance is {num(stats.rate_limit.monthly_limit)}{' '}
                calls, {num(stats.rate_limit.remaining)} remaining. The limit is enforced across
                all keys on the account, not per key.
              </>
            ) : (
              'Limits are enforced across all keys on the account, not per key.'
            )}{' '}
            Requests past it return <span className="font-mono">429</span> and are recorded
            non-billable.
          </div>
        </div>
      </div>

      {/* Create key */}
      {dialog === 'create' && (
        <div className="dialog-backdrop" onClick={() => setDialog(null)}>
          <div
            className="dialog"
            role="dialog"
            aria-modal="true"
            aria-label="Create API key"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="dialog-title">Create API key</div>
            <div className="dialog-body">
              Scope the key to the endpoints it needs. You can revoke it at any time.
            </div>
            <div className="field">
              <label htmlFor="gc-kn">Key name</label>
              <input
                className="input"
                id="gc-kn"
                autoFocus
                placeholder="batch-matcher"
                value={newKeyName}
                onChange={(e) => setNewKeyName(e.target.value)}
              />
            </div>
            <div className="field">
              <label>Scope</label>
              <div className="grid grid-cols-[repeat(auto-fit,minmax(130px,1fr))] gap-2">
                {SCOPES.map((s) => {
                  const on = scopes.includes(s)
                  return (
                    <label key={s} className="radio cursor-pointer">
                      <input
                        type="checkbox"
                        checked={on}
                        onChange={() => toggleScope(s)}
                        className="absolute h-0 w-0 opacity-0"
                      />
                      <span
                        className="block h-3.5 w-3.5 flex-none"
                        style={{
                          border: `1.5px solid ${on ? 'var(--color-accent)' : 'var(--color-divider)'}`,
                          background: on ? 'var(--color-accent)' : 'transparent',
                          boxShadow: on ? 'inset 0 0 0 2px var(--color-bg)' : 'none',
                        }}
                      />
                      {s === '*' ? 'all endpoints' : s}
                    </label>
                  )
                })}
              </div>
            </div>
            <div className="dialog-actions">
              <button type="button" className="btn btn-secondary" onClick={() => setDialog(null)}>
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={createKey}
                disabled={saving}
              >
                {saving ? 'Creating…' : 'Create key'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Key created */}
      {dialog === 'created' && (
        <div className="dialog-backdrop">
          <div className="dialog" role="dialog" aria-modal="true" aria-label="Key created">
            <div className="dialog-title">Key created</div>
            <div className="dialog-body">
              Copy it now — this is the only time the full key is shown.
            </div>
            <div className="break-all bg-[var(--gc-code-bg)] p-3.5 font-mono text-[13px] text-[var(--gc-code-ink)]">
              {createdKey || '(the server did not return a key string)'}
            </div>
            <div className="dialog-actions">
              <button type="button" className="btn btn-secondary" onClick={copyKey}>
                {copied ? 'Copied' : 'Copy key'}
              </button>
              <button type="button" className="btn btn-primary" onClick={() => setDialog(null)}>
                Done
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
