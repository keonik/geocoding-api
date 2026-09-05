import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useEffect } from 'react'
import { ThemeToggle } from '@/components/theme-toggle'

export const Route = createFileRoute('/')({
  component: LandingPage,
})

/** Modernist's "grid" hero variant: split panel, hairline gutters, no gradient. */
const HERO_CURL = `curl "https://api.geocode.dev/api/v1/addresses/search?q=291+N+HIGH+ST" \\
  -H "X-API-Key: $GEOCODE_API_KEY"`

/* Field names below are models.OhioAddress verbatim, abridged to what fits. */
const HERO_JSON = `{
  "success": true,
  "count": 1,
  "data": [
    {
      "full_address": "291 N HIGH ST, COLUMBUS, OH 43215",
      "city": "COLUMBUS",
      "county": "Franklin",
      "postcode": "43215",
      "latitude": 39.96551,
      "longitude": -83.00394
    }
  ]
}`

const CAPABILITIES = [
  {
    path: 'GET /api/v1/geocode/{zip}',
    title: 'Geocode',
    body: 'A ZIP or ZCTA in, the full record out: USPS city and state, county weights, timezone, population, density and the centroid coordinate.',
  },
  {
    path: 'GET /api/v1/addresses/search',
    title: 'Match',
    body: 'Free-form Ohio street addresses matched against parcel records. Abbreviations and suffixes are normalised first, and misspellings fall back to trigram similarity.',
  },
  {
    path: 'GET /api/v1/distance/{a}/{b}',
    title: 'Distance',
    body: 'Great-circle distance between the centroids of any two ZIP codes, in miles and kilometres.',
  },
  {
    path: 'GET /api/v1/nearby/{zip}',
    title: 'Nearby',
    body: 'Every ZIP whose centroid falls inside a radius, nearest first — or a straight yes/no proximity check between two of them.',
  },
  {
    path: 'GET /api/v1/states/lookup',
    title: 'Reverse',
    body: 'The inverse of the rest: a latitude and longitude in, the containing state out, resolved point-in-polygon against real boundary geometry.',
  },
  {
    path: 'GET /api/v1/states/{id}/boundary',
    title: 'Boundaries',
    body: 'State outlines as GeoJSON, ready to hand straight to Leaflet or MapLibre. Simplified for display by default; ask for full source resolution when you need it.',
  },
]

const ENDPOINT_TABLE = [
  ['/api/v1/geocode/{zipcode}', 'One ZIP record, with county weights and centroid'],
  ['/api/v1/search', 'ZIP codes by city name, ordered by population'],
  ['/api/v1/addresses/search', 'Ohio street address matched to a point'],
  ['/api/v1/addresses/{id}', 'One Ohio parcel address by id'],
  ['/api/v1/distance/{from}/{to}', 'Distance between two ZIP centroids'],
  ['/api/v1/nearby/{zipcode}', 'ZIP codes within a radius, nearest first'],
  ['/api/v1/proximity/{center}/{target}', 'Whether a ZIP falls inside a radius'],
  ['/api/v1/cities', 'US cities, filterable by name and state'],
  ['/api/v1/cities/zips', 'Every ZIP code belonging to a city'],
  ['/api/v1/states', 'States and territories, with census region and division'],
  ['/api/v1/states/lookup', 'The state containing a coordinate'],
  ['/api/v1/states/{id}', 'One state by FIPS code, abbreviation or name'],
  ['/api/v1/states/{id}/boundary', 'State outline as a GeoJSON Feature'],
]

function LandingPage() {
  const navigate = useNavigate()
  const token = localStorage.getItem('authToken')

  // If logged in, redirect to the usage screen
  useEffect(() => {
    if (token) {
      navigate({ to: '/usage' })
    }
  }, [token, navigate])

  return (
    <div className="min-h-screen bg-background text-foreground">
      <div className="nav flex-wrap gap-[14px] px-[clamp(16px,4vw,40px)] py-3">
        <span className="nav-brand">GeoCode API</span>
        <a href="/docs" target="_blank" rel="noopener noreferrer">
          Docs
        </a>
        <Link to="/auth/signin">Sign in</Link>
        <ThemeToggle />
        <Link to="/auth/signup" className="btn btn-primary">
          Get a key
        </Link>
      </div>

      {/* Hero — two panels separated by a 2px gutter showing the page ground */}
      <div className="flex flex-wrap gap-[2px] border-b-2 border-[var(--color-divider)] bg-[var(--color-divider)]">
        <div className="min-w-0 flex-[1_1_380px] bg-[var(--color-bg)] px-[clamp(20px,4vw,40px)] pt-[clamp(40px,7vw,76px)] pb-[clamp(36px,6vw,64px)]">
          <div className="mb-6 text-[11px] uppercase tracking-[0.14em] text-[var(--color-accent-700)]">
            US ZIP codes · Ohio parcel addresses · state boundaries
          </div>
          <h1 className="m-0 mb-6 max-w-[17ch] text-[clamp(34px,6.5vw,64px)] leading-[0.98] tracking-[-0.03em] text-pretty">
            Every address, resolved to a point.
          </h1>
          <p className="m-0 mb-7 max-w-[48ch] text-lg leading-[1.55]">
            Look up a ZIP or a city, match a messy Ohio address list against canonical
            parcel records, turn a coordinate back into a place, measure distance between
            any two points. One REST call, one key, metered per call.
          </p>
          <div className="flex flex-wrap gap-3">
            <Link to="/auth/signup" className="btn btn-primary px-5 py-[13px] text-[15px]">
              Create an account
            </Link>
            <Link to="/playground" className="btn btn-secondary px-5 py-[13px] text-[15px]">
              Open the playground
            </Link>
          </div>
        </div>
        <div className="flex min-w-0 flex-[1_1_340px] flex-col justify-center bg-[var(--color-surface)] p-[clamp(20px,4vw,40px)]">
          <div className="mb-3 text-[11px] uppercase tracking-[0.1em] text-[var(--color-accent-700)]">
            One call
          </div>
          <pre className="m-0 mb-5 [overflow-wrap:anywhere] whitespace-pre-wrap bg-[var(--gc-code-bg)] p-[18px] font-mono text-[13px] leading-[1.7] text-[var(--gc-code-ink)]">
            {HERO_CURL}
          </pre>
          <pre className="m-0 whitespace-pre-wrap border border-[var(--color-divider)] p-[18px] font-mono text-[12.5px] leading-[1.65]">
            {HERO_JSON}
          </pre>
        </div>
      </div>

      {/* Capabilities */}
      {/* Capped at three columns so six cards always fill their rows. auto-fit
          would give five-plus-one on a wide screen, and the gap technique below
          would paint the empty cell divider-coloured. Hairlines come from a 1px
          gap over a divider-coloured ground, as the hero does, which stays
          correct however the cards wrap. */}
      <div className="grid grid-cols-1 gap-[1px] border-b-2 border-[var(--color-divider)] bg-[var(--color-divider)] md:grid-cols-2 lg:grid-cols-3">
        {CAPABILITIES.map((c) => (
          <div
            key={c.path}
            className="bg-[var(--color-bg)] px-[clamp(20px,3vw,28px)] py-[clamp(24px,3vw,32px)]"
          >
            <div className="mb-[14px] font-mono text-xs text-[var(--color-accent-700)]">
              {c.path}
            </div>
            <h4 className="m-0 mb-2 text-[21px]">{c.title}</h4>
            <p className="m-0 text-sm leading-[1.55] opacity-80">{c.body}</p>
          </div>
        ))}
      </div>

      {/* Metering — what is actually true of the usage pipeline */}
      <div className="flex flex-wrap gap-[2px] border-b-2 border-[var(--color-divider)] bg-[var(--color-divider)]">
        <div className="min-w-0 flex-[1_1_380px] bg-[var(--color-bg)] px-[clamp(20px,4vw,40px)] py-[clamp(32px,5vw,56px)]">
          <h2 className="m-0 mb-4 max-w-[22ch] text-[clamp(26px,4vw,34px)]">
            Metered per call, visible per endpoint.
          </h2>
          <p className="m-0 mb-6 max-w-[52ch] text-base leading-[1.6]">
            Every authenticated request is recorded with its endpoint, status code and
            response time. Your usage page breaks the month down per endpoint, so you can
            see which calls cost what. Requests past your monthly limit return{' '}
            <span className="font-mono">429</span> and are recorded non-billable.
          </p>
          <div className="hr" />
          <div className="grid grid-cols-[repeat(auto-fit,minmax(110px,1fr))] gap-6">
            <div>
              <div className="font-display text-3xl font-extrabold">19</div>
              <div className="text-xs opacity-65">read endpoints</div>
            </div>
            <div>
              <div className="font-display text-3xl font-extrabold">ZIP + ZCTA</div>
              <div className="text-xs opacity-65">national coverage</div>
            </div>
            <div>
              <div className="font-display text-3xl font-extrabold">Ohio</div>
              <div className="text-xs opacity-65">parcel-level addresses</div>
            </div>
          </div>
        </div>
        <div className="min-w-0 flex-[1_1_340px] bg-[var(--color-surface)] px-[clamp(20px,4vw,40px)] py-[clamp(32px,5vw,56px)]">
          <div className="mb-4 text-[11px] uppercase tracking-[0.1em] text-[var(--color-accent-700)]">
            The read surface
          </div>
          <div className="overflow-x-auto">
            <table className="table min-w-[420px]">
              <thead>
                <tr>
                  <th>Endpoint</th>
                  <th>Returns</th>
                </tr>
              </thead>
              <tbody>
                {ENDPOINT_TABLE.map(([path, returns]) => (
                  <tr key={path}>
                    <td className="font-mono text-[13px]">{path}</td>
                    <td>{returns}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </div>

      {/* Closer */}
      <div className="border-b-2 border-[var(--color-divider)] bg-[var(--color-accent)] px-[clamp(20px,4vw,40px)] py-[clamp(44px,7vw,72px)] text-[#f3f2f2]">
        <div className="max-w-[1180px]">
          <h2 className="m-0 mb-5 max-w-[24ch] text-[clamp(30px,5.5vw,46px)] tracking-[-0.025em]">
            Issue a key and make your first call.
          </h2>
          <Link
            to="/auth/signup"
            className="on-accent inline-block border-0 bg-[#f3f2f2] px-[22px] py-[14px] font-display text-[15px] font-extrabold text-[#201e1d]"
          >
            Create an account
          </Link>
        </div>
      </div>

      <div className="flex flex-wrap gap-6 border-t-2 border-[var(--color-divider)] px-[clamp(20px,4vw,40px)] py-7 text-xs opacity-70">
        <span>© 2026 GeoCode API</span>
        <a href="/docs" target="_blank" rel="noopener noreferrer">
          Documentation
        </a>
        <a href="/api-docs.yaml" target="_blank" rel="noopener noreferrer">
          OpenAPI spec
        </a>
      </div>
    </div>
  )
}
