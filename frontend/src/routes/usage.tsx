import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import { AppNav } from '@/components/app-nav'
import { usageAPI, type DailyUsage, type EndpointUsage } from '@/api/usage'
import type { UsageStats } from '@/types/api'

export const Route = createFileRoute('/usage')({
  component: UsagePage,
})

const RANGES = [7, 30, 90] as const
const num = (n: number) => n.toLocaleString('en-US')

/** 'YYYY-MM-DD' -> 'M/D', without dragging in a date library. */
function shortDay(iso: string) {
  const [, m, d] = iso.split('-')
  return m && d ? `${Number(m)}/${Number(d)}` : iso
}

function UsagePage() {
  const navigate = useNavigate()
  const [range, setRange] = useState<number>(30)
  const [stats, setStats] = useState<UsageStats | null>(null)
  const [daily, setDaily] = useState<DailyUsage[]>([])
  const [endpoints, setEndpoints] = useState<EndpointUsage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!localStorage.getItem('authToken')) {
      navigate({ to: '/auth/signin' })
      return
    }
    let cancelled = false
    setLoading(true)
    Promise.all([
      usageAPI.getStats(),
      usageAPI.getDailyUsage(range),
      usageAPI.getEndpointUsage(range),
    ])
      .then(([s, d, e]) => {
        if (cancelled) return
        setStats(s.data ?? null)
        setDaily(d.data ?? [])
        setEndpoints(e.data ?? [])
        setError('')
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load usage')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [range, navigate])

  const totals = useMemo(() => {
    const total = daily.reduce((a, d) => a + d.total_calls, 0)
    const billable = daily.reduce((a, d) => a + d.billable_calls, 0)
    const success = endpoints.reduce((a, e) => a + e.success_count, 0)
    const errors = endpoints.reduce((a, e) => a + e.error_count, 0)
    const peak = daily.reduce((a, d) => Math.max(a, d.total_calls), 0)
    return { total, billable, success, errors, peak }
  }, [daily, endpoints])

  const limit = stats?.rate_limit.monthly_limit ?? 0
  const used = stats?.rate_limit.current_usage ?? 0
  const quotaPct = limit > 0 ? (used / limit) * 100 : 0
  const billablePct = totals.total > 0 ? Math.round((totals.billable / totals.total) * 100) : 0
  const decided = totals.success + totals.errors
  const successPct = decided > 0 ? ((totals.success / decided) * 100).toFixed(1) : '—'

  const endpointMix = useMemo(() => {
    const sum = endpoints.reduce((a, e) => a + e.total_calls, 0)
    return [...endpoints]
      .sort((a, b) => b.total_calls - a.total_calls)
      .map((e) => ({
        name: e.endpoint,
        pct: sum > 0 ? (e.total_calls / sum) * 100 : 0,
      }))
  }, [endpoints])

  return (
    <div className="min-h-screen bg-background text-foreground">
      <AppNav />

      <div className="flex flex-wrap items-end justify-between gap-6 px-[clamp(20px,4vw,40px)] pt-8 pb-5">
        <div>
          <h2 className="m-0 mb-1 text-[clamp(26px,4vw,32px)]">Usage</h2>
          <div className="text-[13px] opacity-70">
            {loading
              ? 'Loading…'
              : stats
                ? `${stats.usage_summary.month} · ${num(limit)} calls included`
                : 'Usage summary unavailable'}
          </div>
        </div>
        <div className="seg">
          {RANGES.map((r) => (
            <label key={r} className="seg-opt">
              <input
                type="radio"
                name="gc-range"
                checked={range === r}
                onChange={() => setRange(r)}
              />
              Last {r} days
            </label>
          ))}
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="mx-[clamp(20px,4vw,40px)] mb-4 border-l-2 border-[var(--color-accent)] bg-[var(--color-accent-100)] p-3 text-sm text-[var(--color-accent-800)]"
        >
          {error}
        </div>
      )}

      {/* KPI strip */}
      <div className="grid grid-cols-[repeat(auto-fit,minmax(215px,1fr))] border-y-2 border-[var(--color-divider)]">
        <div className="border-r border-[var(--color-divider)] px-7 py-6">
          <div className="mb-2.5 text-[11px] uppercase tracking-[0.09em] opacity-60">
            Total calls
          </div>
          <div className="font-display text-[38px] font-extrabold leading-none">
            {num(totals.total)}
          </div>
          <div className="mt-1.5 text-xs opacity-65">last {range} days</div>
        </div>
        <div className="border-r border-[var(--color-divider)] px-7 py-6">
          <div className="mb-2.5 text-[11px] uppercase tracking-[0.09em] opacity-60">
            Billable calls
          </div>
          <div className="font-display text-[38px] font-extrabold leading-none">
            {num(totals.billable)}
          </div>
          <div className="mt-1.5 text-xs opacity-65">
            {billablePct}% of calls · over-limit not billed
          </div>
        </div>
        <div className="border-r border-[var(--color-divider)] px-7 py-6">
          <div className="mb-2.5 text-[11px] uppercase tracking-[0.09em] opacity-60">
            Quota burn
          </div>
          <div className="font-display text-[38px] font-extrabold leading-none">
            {limit > 0 ? `${quotaPct.toFixed(0)}%` : '—'}
          </div>
          <div className="mt-3 h-2.5 bg-[var(--color-neutral-300)]">
            <div
              className="h-full bg-[var(--color-accent)]"
              style={{ width: `${Math.min(100, quotaPct)}%` }}
            />
          </div>
          <div className="mt-2 text-xs opacity-65">
            {num(used)} of {num(limit)} this month
          </div>
        </div>
        <div className="px-7 py-6">
          <div className="mb-2.5 text-[11px] uppercase tracking-[0.09em] opacity-60">
            Success rate
          </div>
          <div className="font-display text-[38px] font-extrabold leading-none">
            {successPct === '—' ? '—' : `${successPct}%`}
          </div>
          <div className="mt-1.5 text-xs opacity-65">
            {num(totals.errors)} errors in {num(decided)} recorded
          </div>
        </div>
      </div>

      {/* Chart + endpoint mix */}
      <div className="flex flex-wrap gap-[2px] bg-[var(--color-divider)]">
        <div className="min-w-0 flex-[1_1_440px] bg-[var(--color-bg)] px-[clamp(20px,4vw,40px)] pt-7 pb-8">
          <div className="mb-5 flex items-baseline justify-between">
            <h4 className="m-0 text-[19px]">Calls per day</h4>
            <div className="flex gap-4 text-[11px] uppercase tracking-[0.06em] opacity-70">
              <span className="flex items-center gap-1.5">
                <span className="block h-2.5 w-2.5 bg-[var(--color-accent)]" />
                billable
              </span>
              <span className="flex items-center gap-1.5">
                <span className="block h-2.5 w-2.5 bg-[var(--color-neutral-400)]" />
                total
              </span>
            </div>
          </div>
          <div className="flex h-[220px] items-end gap-[3px] border-b-2 border-[var(--color-divider)]">
            {daily.map((d) => (
              <div
                key={d.date}
                className="flex h-full min-w-[2px] flex-1 items-end"
                title={`${shortDay(d.date)} · ${num(d.total_calls)} calls, ${num(d.billable_calls)} billable`}
              >
                <div
                  className="flex w-full items-end bg-[var(--color-neutral-400)]"
                  style={{
                    height: totals.peak > 0 ? `${(d.total_calls / totals.peak) * 100}%` : '0%',
                  }}
                >
                  <div
                    className="w-full bg-[var(--color-accent)]"
                    style={{
                      height:
                        d.total_calls > 0 ? `${(d.billable_calls / d.total_calls) * 100}%` : '0%',
                    }}
                  />
                </div>
              </div>
            ))}
          </div>
          <div className="mt-2 flex justify-between font-mono text-[11px] opacity-60">
            <span>{daily.length ? shortDay(daily[0].date) : ''}</span>
            <span>{daily.length ? shortDay(daily[daily.length - 1].date) : ''}</span>
          </div>
          {!loading && daily.length === 0 && (
            <div className="mt-4 text-sm opacity-65">No calls recorded in this range.</div>
          )}
        </div>

        <div className="min-w-0 flex-[1_1_300px] bg-[var(--color-surface)] px-[clamp(20px,4vw,32px)] pt-7 pb-8">
          <h4 className="m-0 mb-1.5 text-[19px]">Endpoint mix</h4>
          <div className="mb-6 text-xs opacity-70">
            Share of the {num(totals.total)} calls in this period, by endpoint group.
          </div>
          <div className="flex flex-col gap-[18px]">
            {endpointMix.map((e) => (
              <div key={e.name}>
                <div className="mb-1.5 flex justify-between text-[13px]">
                  <span className="font-mono">{e.name}</span>
                  <span className="opacity-70">{e.pct.toFixed(1)}%</span>
                </div>
                <div className="h-3 bg-[var(--color-neutral-300)]">
                  <div
                    className="h-full bg-[var(--color-accent)]"
                    style={{ width: `${e.pct}%` }}
                  />
                </div>
              </div>
            ))}
          </div>
          {!loading && endpointMix.length === 0 && (
            <div className="text-sm opacity-65">No endpoint activity yet.</div>
          )}
          <div className="hr" />
          <div className="text-xs leading-relaxed opacity-75">
            Calls past your monthly limit return{' '}
            <span className="font-mono">429</span> and are recorded non-billable. Everything
            else that reaches an endpoint counts, including responses that error.
          </div>
        </div>
      </div>

      {/* By endpoint */}
      <div className="border-t-2 border-[var(--color-divider)] px-[clamp(20px,4vw,40px)] pt-7 pb-12">
        <h4 className="m-0 mb-4 text-[19px]">By endpoint</h4>
        <div className="overflow-x-auto">
          <table className="table min-w-[720px]">
            <thead>
              <tr>
                <th>Endpoint</th>
                <th className="text-right">Calls</th>
                <th className="text-right">Billable</th>
                <th className="text-right">Avg response</th>
                <th className="text-right">Success</th>
                <th className="text-right">Errors</th>
              </tr>
            </thead>
            <tbody>
              {endpoints.map((e) => {
                const seen = e.success_count + e.error_count
                return (
                  <tr key={e.endpoint}>
                    <td className="font-mono text-[13px]">{e.endpoint}</td>
                    <td className="text-right">{num(e.total_calls)}</td>
                    <td className="text-right">{num(e.billable_calls)}</td>
                    <td className="text-right">{Math.round(e.avg_response_time)} ms</td>
                    <td className="text-right">
                      {seen > 0 ? `${((e.success_count / seen) * 100).toFixed(1)}%` : '—'}
                    </td>
                    <td className="text-right">{num(e.error_count)}</td>
                  </tr>
                )
              })}
              {!loading && endpoints.length === 0 && (
                <tr>
                  <td colSpan={6} className="opacity-65">
                    Nothing recorded yet — make a call from the Playground.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
