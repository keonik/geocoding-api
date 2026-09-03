import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import { AppNav } from '@/components/app-nav'

export const Route = createFileRoute('/playground')({
  component: PlaygroundPage,
})

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || ''

type Endpoint = {
  path: string
  label: string
  placeholder: string
  initial: string
  /** Turns the single input box into a real request path. */
  build: (input: string) => string
}

/** Split "43215/45202" or "43215 / 45202" into its two halves. */
function pair(input: string): [string, string] {
  const [a = '', b = ''] = input.split('/').map((s) => s.trim())
  return [a, b]
}

const ENDPOINTS: Endpoint[] = [
  {
    path: '/api/v1/geocode/{zipcode}',
    label: 'zipcode',
    placeholder: '43215',
    initial: '43215',
    build: (i) => `/api/v1/geocode/${encodeURIComponent(i.trim())}`,
  },
  {
    path: '/api/v1/search',
    label: 'query string',
    placeholder: 'city=Springfield&state=OH&limit=5',
    initial: 'city=Springfield&state=OH&limit=5',
    build: (i) => `/api/v1/search?${i.trim().replace(/^\?/, '')}`,
  },
  {
    path: '/api/v1/addresses/search',
    label: 'address (q)',
    placeholder: '291 N HIGH ST COLUMBUS',
    initial: '291 N HIGH ST COLUMBUS',
    build: (i) => `/api/v1/addresses/search?q=${encodeURIComponent(i.trim())}`,
  },
  {
    path: '/api/v1/distance/{from}/{to}',
    label: 'from / to',
    placeholder: '43215 / 45202',
    initial: '43215 / 45202',
    build: (i) => {
      const [a, b] = pair(i)
      return `/api/v1/distance/${encodeURIComponent(a)}/${encodeURIComponent(b)}`
    },
  },
  {
    path: '/api/v1/nearby/{zipcode}',
    label: 'zipcode (+ query)',
    placeholder: '43215?radius=5&limit=3',
    initial: '43215?radius=5&limit=3',
    build: (i) => {
      const [zip, qs] = i.trim().split('?')
      return `/api/v1/nearby/${encodeURIComponent(zip.trim())}${qs ? `?${qs}` : ''}`
    },
  },
  {
    path: '/api/v1/proximity/{center}/{target}',
    label: 'center / target (+ query)',
    placeholder: '43215 / 43206?radius=3',
    initial: '43215 / 43206?radius=3',
    build: (i) => {
      const [body, qs] = i.trim().split('?')
      const [a, b] = pair(body)
      return `/api/v1/proximity/${encodeURIComponent(a)}/${encodeURIComponent(b)}${qs ? `?${qs}` : ''}`
    },
  },
]

type Result = { status: number; ms: number; body: string; ok: boolean }

function PlaygroundPage() {
  const navigate = useNavigate()
  const [sel, setSel] = useState(0)
  const [input, setInput] = useState(ENDPOINTS[0].initial)
  const [apiKey, setApiKey] = useState('')
  const [result, setResult] = useState<Result | null>(null)
  const [running, setRunning] = useState(false)

  useEffect(() => {
    if (!localStorage.getItem('authToken')) navigate({ to: '/auth/signin' })
  }, [navigate])

  const endpoint = ENDPOINTS[sel]
  const url = useMemo(() => {
    try {
      return endpoint.build(input)
    } catch {
      return endpoint.path
    }
  }, [endpoint, input])

  const curl = useMemo(() => {
    const shown = apiKey ? `${apiKey.slice(0, 8)}…` : '$GEOCODE_API_KEY'
    return `curl "${API_BASE_URL || window.location.origin}${url}" \\\n  -H "X-API-Key: ${shown}"`
  }, [url, apiKey])

  const send = async () => {
    if (!apiKey.trim()) {
      // Explain in the response pane rather than leaving a dead, faded button
      // with no feedback: the reason is not obvious from the control itself.
      setResult({
        status: 0,
        ms: 0,
        ok: false,
        body:
          '// An API key is required.\n' +
          '//\n' +
          '// These endpoints authenticate with X-API-Key. Your signed-in\n' +
          '// session is not accepted here. Create a key on the keys page\n' +
          '// and paste it above -- the full value is shown only once.',
      })
      return
    }
    setRunning(true)
    // The protected group runs middleware.APIKeyAuth, which reads an
    // "Authorization: Bearer …" value as an API key rather than as a session
    // token. Your login JWT is therefore not usable here -- it fails
    // ValidateAPIKey with a 401 -- so a real key is required.
    const headers: Record<string, string> = { 'X-API-Key': apiKey.trim() }

    const started = performance.now()
    try {
      const res = await fetch(`${API_BASE_URL}${url}`, { headers })
      const text = await res.text()
      let body = text
      try {
        body = JSON.stringify(JSON.parse(text), null, 2)
      } catch {
        // Not JSON — show whatever came back verbatim.
      }
      setResult({
        status: res.status,
        ms: Math.round(performance.now() - started),
        body,
        ok: res.ok,
      })
    } catch (err) {
      setResult({
        status: 0,
        ms: Math.round(performance.now() - started),
        body: err instanceof Error ? err.message : 'Network error',
        ok: false,
      })
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <AppNav />

      <div className="border-b-2 border-[var(--color-divider)] px-[clamp(20px,4vw,40px)] pt-8 pb-5">
        <h2 className="m-0 mb-1 text-[clamp(26px,4vw,32px)]">Playground</h2>
        <div className="text-[13px] opacity-70">
          Requests are sent from this browser to the live API. They are recorded and count
          toward your monthly limit.
        </div>
      </div>

      <div className="flex flex-wrap gap-[2px] bg-[var(--color-divider)]">
        <div className="min-w-0 flex-[1_1_320px] bg-[var(--color-bg)] px-[clamp(20px,4vw,32px)] pt-7 pb-10">
          <div className="mb-2.5 text-[11px] uppercase tracking-[0.1em] opacity-60">Endpoint</div>
          <div className="mb-6 flex flex-col gap-[2px]">
            {ENDPOINTS.map((e, i) => (
              <button
                key={e.path}
                type="button"
                onClick={() => {
                  setSel(i)
                  setInput(e.initial)
                  setResult(null)
                }}
                className={`cursor-pointer border-0 px-3 py-2.5 text-left font-mono text-[13px] ${
                  sel === i ? 'on-accent' : ''
                }`}
                style={{
                  background: sel === i ? 'var(--color-accent)' : 'var(--color-surface)',
                  color: sel === i ? '#f8f4f4' : 'var(--color-text)',
                }}
              >
                {e.path}
              </button>
            ))}
          </div>

          <div className="field mb-4">
            <label htmlFor="gc-q">{endpoint.label}</label>
            <input
              className="input"
              id="gc-q"
              value={input}
              placeholder={endpoint.placeholder}
              onChange={(e) => setInput(e.target.value)}
            />
          </div>

          <div className="field mb-5">
            <label htmlFor="gc-key">API key (required)</label>
            <input
              className="input font-mono"
              id="gc-key"
              value={apiKey}
              placeholder="gc_live_…"
              onChange={(e) => setApiKey(e.target.value)}
            />
            <div className="mt-1.5 text-[11px] opacity-60">
              These endpoints take an API key, not your login session. Keys are shown once at
              creation and stored only as a truncated preview, so paste one here — create a new
              one on the keys page if you no longer have it.
            </div>
          </div>

          <button
            type="button"
            className="btn btn-primary px-[18px] py-[11px]"
            onClick={send}
            disabled={running}
          >
            {running ? 'Sending…' : 'Send request'}
          </button>

          <div className="hr" />
          <div className="mb-2 text-[11px] uppercase tracking-[0.1em] opacity-60">Request</div>
          <pre className="m-0 [overflow-wrap:anywhere] whitespace-pre-wrap bg-[var(--gc-code-bg)] p-4 font-mono text-[12.5px] leading-[1.7] text-[var(--gc-code-ink)]">
            {curl}
          </pre>
        </div>

        <div className="min-w-0 flex-[1_1_380px] bg-[var(--color-surface)] px-[clamp(20px,4vw,32px)] pt-7 pb-10">
          <div className="mb-4 flex flex-wrap items-center gap-4">
            <div className="text-[11px] uppercase tracking-[0.1em] opacity-60">Response</div>
            {result && (
              <>
                <span
                  className={`tag font-mono ${result.ok ? 'tag-neutral' : 'tag-outline'}`}
                >
                  {result.status === 0 ? 'network error' : result.status}
                </span>
                <span className="font-mono text-xs opacity-70">{result.ms} ms</span>
              </>
            )}
            {!result && (
              <span className="font-mono text-xs opacity-70">press send to run this request</span>
            )}
          </div>
          <pre className="m-0 min-h-[360px] overflow-x-auto whitespace-pre-wrap border border-[var(--color-divider)] bg-[var(--color-bg)] p-[18px] font-mono text-[12.5px] leading-[1.7]">
            {result ? result.body : '// response appears here'}
          </pre>
        </div>
      </div>
    </div>
  )
}
