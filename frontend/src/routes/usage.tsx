import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import { AppNav } from '@/components/app-nav'
import { usageAPI, type DailyUsage, type EndpointUsage, type KeyUsage } from '@/api/usage'
import type { UsageStats } from '@/types/api'

export const Route = createFileRoute('/usage')({
  component: UsagePage,
})

const RANGES = [7, 30, 90, 365] as const
const rangeLabel = (r: number) => (r === 365 ? 'Last 12 months' : `Last ${r} days`)
const num = (n: number) => n.toLocaleString('en-US')

/**
 * DailyUsage.Date is a Go string scanned from a Postgres DATE, so lib/pq hands
 * database/sql a time.Time and it arrives as RFC3339 ("2026-09-02T00:00:00Z"),
 * not "YYYY-MM-DD". Parse it rather than splitting on '-', which yielded a day
 * of "02T00:00:00Z" and rendered every label as "9/NaN".
 */
function shortDay(value: string) {
  const d = new Date(value)
  return Number.isNaN(d.getTime())
    ? value
    : d.toLocaleDateString('en-US', { month: 'numeric', day: 'numeric' })
}

function UsagePage() {
  const navigate = useNavigate()
  const [range, setRange] = useState<number>(30)
  const [stats, setStats] = useState<UsageStats | null>(null)
  const [daily, setDaily] = useState<DailyUsage[]>([])
  const [endpoints, setEndpoints] = useState<EndpointUsage[]>([])
  const [keyUsage, setKeyUsage] = useState<KeyUsage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!localStorage.getItem('authToken')) {
      navigate({ to: '/auth/signin' })
      return
    }
    let cancelled = false
    setLoading(true)
    // allSettled, not all: these three endpoints back three independent panels,
    // and Promise.all would let one failure blank the entire page without
    // saying which request died.
    Promise.allSettled([
      usageAPI.getStats(),
      usageAPI.getDailyUsage(range),
      usageAPI.getEndpointUsage(range),
      usageAPI.getKeyUsage(range),
    ])
      .then(([s, d, e, k]) => {
        if (cancelled) return
        const failures: string[] = []
        const reason = (r: PromiseRejectedResult) =>
          r.reason instanceof Error ? r.reason.message : String(r.reason)

        if (s.status === 'fulfilled') setStats(s.value.data ?? null)
        else failures.push(`/user/usage: ${reason(s)}`)

        if (d.status === 'fulfilled') {
          // GetDailyUsage orders by date DESC (newest first). The chart and its
          // axis labels read oldest -> newest, so flip it here.
          setDaily([...(d.value.data ?? [])].reverse())
        } else failures.push(`/user/usage/daily: ${reason(d)}`)

        if (e.status === 'fulfilled') setEndpoints(e.value.data ?? [])
        else failures.push(`/user/usage/endpoints: ${reason(e)}`)

        if (k.status === 'fulfilled') setKeyUsage(k.value.data ?? [])
        else failures.push(`/user/usage/keys: ${reason(k)}`)

        setError(failures.join(' · '))
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

  // CheckRateLimit returns -1 for monthly_limit on plans with no cap.
  const limit = stats?.rate_limit.monthly_limit ?? 0
  const unlimited = limit < 0
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
                ? `${stats.usage_summary.month} · ${
                    unlimited ? 'no monthly limit' : `${num(limit)} calls included`
                  }`
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
              {rangeLabel(r)}
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
            {unlimited ? 'This month' : 'Quota burn'}
          </div>
          <div className="font-display text-[38px] font-extrabold leading-none">
            {unlimited ? num(used) : limit > 0 ? `${quotaPct.toFixed(0)}%` : '—'}
          </div>
          {!unlimited && (
            <div className="mt-3 h-2.5 bg-[var(--color-neutral-300)]">
              <div
                className="h-full bg-[var(--color-accent)]"
                style={{ width: `${Math.min(100, quotaPct)}%` }}
              />
            </div>
          )}
          <div className="mt-2 text-xs opacity-65">
            {unlimited
              ? `calls in ${stats?.usage_summary.month ?? 'this month'} — no limit on this plan`
              : `${num(used)} of ${num(limit)} in ${
                  stats?.usage_summary.month ?? 'this month'
                } — calendar month, not the range above`}
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

      {/* By key */}
      <div className="border-t-2 border-[var(--color-divider)] px-[clamp(20px,4vw,40px)] pt-7 pb-10">
        <div className="mb-4 flex flex-wrap items-baseline justify-between gap-3">
          <h4 className="m-0 text-[19px]">By key</h4>
          <div className="text-xs opacity-65">
            Usage is attributed to the key that made the call, not to the signed-in user.
          </div>
        </div>
        <div className="overflow-x-auto">
          <table className="table min-w-[720px]">
            <thead>
              <tr>
                <th>Key</th>
                <th className="text-right">Calls</th>
                <th className="text-right">Billable</th>
                <th className="text-right">Avg response</th>
                <th className="text-right">Errors</th>
                <th>Last call</th>
              </tr>
            </thead>
            <tbody>
              {keyUsage.map((k) => (
                <tr key={k.key_id}>
                  <td>
                    <span className="font-semibold">{k.name}</span>{' '}
                    <span className="font-mono text-xs opacity-60">{k.key_preview}</span>
                    {!k.is_active && (
                      <span className="tag tag-neutral ml-2">revoked</span>
                    )}
                  </td>
                  <td className="text-right">{num(k.total_calls)}</td>
                  <td className="text-right">{num(k.billable_calls)}</td>
                  <td className="text-right">
                    {k.total_calls > 0 ? `${Math.round(k.avg_response_time)} ms` : '—'}
                  </td>
                  <td className="text-right">{num(k.error_count)}</td>
                  <td className="opacity-75">
                    {k.last_call
                      ? new Date(k.last_call).toLocaleString('en-US', {
                          month: 'short',
                          day: 'numeric',
                          hour: 'numeric',
                          minute: '2-digit',
                        })
                      : 'no calls in range'}
                  </td>
                </tr>
              ))}
              {!loading && keyUsage.length === 0 && (
                <tr>
                  <td colSpan={6} className="opacity-65">
                    No API keys on this account yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
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
