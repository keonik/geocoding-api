import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { usageAPI, type DailyUsage, type EndpointUsage } from '@/api/usage'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Toaster } from '@/components/ui/toaster'
import { ArrowLeft, TrendingUp, Activity, Clock } from 'lucide-react'
import { ThemeToggle } from '@/components/theme-toggle'
import {
  LineChart,
  Line,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  PieChart,
  Pie,
  Cell,
} from 'recharts'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  ChartLegend,
  ChartLegendContent,
  type ChartConfig,
} from '@/components/ui/chart'

export const Route = createFileRoute('/usage')({
  beforeLoad: () => {
    const token = localStorage.getItem('authToken')
    if (!token) {
      throw redirect({ to: '/auth/signin' })
    }
  },
  component: UsageAnalytics,
})

const COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899']

// Chart configurations
const dailyChartConfig = {
  total_calls: {
    label: 'Total Calls',
    color: 'hsl(var(--chart-1))',
  },
  billable_calls: {
    label: 'Billable Calls',
    color: 'hsl(var(--chart-2))',
  },
} satisfies ChartConfig

const endpointChartConfig = {
  total_calls: {
    label: 'Total Calls',
    color: 'hsl(var(--chart-1))',
  },
  billable_calls: {
    label: 'Billable',
    color: 'hsl(var(--chart-2))',
  },
} satisfies ChartConfig

const responseTimeConfig = {
  avg_response_time: {
    label: 'Avg Response (ms)',
    color: 'hsl(var(--chart-3))',
  },
} satisfies ChartConfig

const successErrorConfig = {
  success_count: {
    label: 'Success',
    color: 'hsl(var(--chart-2))',
  },
  error_count: {
    label: 'Errors',
    color: 'hsl(var(--chart-5))',
  },
} satisfies ChartConfig

function UsageAnalytics() {
  const navigate = useNavigate()
  const [dailyUsage, setDailyUsage] = useState<DailyUsage[]>([])
  const [endpointUsage, setEndpointUsage] = useState<EndpointUsage[]>([])
  const [loading, setLoading] = useState(true)
  const [days, setDays] = useState(30)

  const user = JSON.parse(localStorage.getItem('user') || '{}')

  useEffect(() => {
    loadUsageData()
  }, [days])

  const loadUsageData = async () => {
    try {
      setLoading(true)
      const [dailyResponse, endpointResponse] = await Promise.all([
        usageAPI.getDailyUsage(days),
        usageAPI.getEndpointUsage(days),
      ])

      if (dailyResponse.success && dailyResponse.data) {
        // Reverse to show oldest to newest for charts
        setDailyUsage([...dailyResponse.data].reverse())
      }

      if (endpointResponse.success && endpointResponse.data) {
        setEndpointUsage(endpointResponse.data)
      }
    } catch (err) {
      console.error('Error loading usage data:', err)
    } finally {
      setLoading(false)
    }
  }

  const totalCalls = dailyUsage.reduce((sum, day) => sum + day.total_calls, 0)
  const totalBillable = dailyUsage.reduce((sum, day) => sum + day.billable_calls, 0)
  const avgCallsPerDay = dailyUsage.length > 0 ? Math.round(totalCalls / dailyUsage.length) : 0
  
  const avgResponseTime = endpointUsage.length > 0
    ? Math.round(
        endpointUsage.reduce((sum, ep) => sum + ep.avg_response_time, 0) / endpointUsage.length
      )
    : 0

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-lg">Loading analytics...</div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="bg-card border-b">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4 flex justify-between items-center">
          <div className="flex items-center gap-4">
            <Button variant="ghost" size="icon" onClick={() => navigate({ to: '/dashboard' })}>
              <ArrowLeft className="h-5 w-5" />
            </Button>
            <div>
              <h1 className="text-2xl font-bold">Usage Analytics</h1>
              <p className="text-sm text-muted-foreground">{user.email}</p>
            </div>
          </div>
          <div className="flex items-center space-x-4">
            <select
              value={days}
              onChange={(e) => setDays(Number(e.target.value))}
              className="px-3 py-2 border rounded-md text-sm bg-background"
            >
              <option value={7}>Last 7 days</option>
              <option value={30}>Last 30 days</option>
              <option value={60}>Last 60 days</option>
              <option value={90}>Last 90 days</option>
            </select>
            <ThemeToggle />
          </div>
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {/* Summary Stats */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Calls</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{totalCalls.toLocaleString()}</div>
              <p className="text-xs text-muted-foreground">Last {days} days</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Billable Calls</CardTitle>
              <TrendingUp className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{totalBillable.toLocaleString()}</div>
              <p className="text-xs text-muted-foreground">
                {totalCalls > 0 ? Math.round((totalBillable / totalCalls) * 100) : 0}% of total
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Avg Daily Calls</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{avgCallsPerDay.toLocaleString()}</div>
              <p className="text-xs text-muted-foreground">Per day average</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Avg Response Time</CardTitle>
              <Clock className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{avgResponseTime}ms</div>
              <p className="text-xs text-muted-foreground">Average across all endpoints</p>
            </CardContent>
          </Card>
        </div>

        {/* Daily Usage Chart */}
        <Card className="mb-8">
          <CardHeader>
            <CardTitle>Daily API Calls</CardTitle>
            <CardDescription>API usage over time</CardDescription>
          </CardHeader>
          <CardContent>
            <ChartContainer config={dailyChartConfig} className="min-h-[300px] w-full">
              <LineChart data={dailyUsage} accessibilityLayer>
                <CartesianGrid strokeDasharray="3 3" vertical={false} />
                <XAxis
                  dataKey="date"
                  tickLine={false}
                  axisLine={false}
                  tickMargin={8}
                  tickFormatter={(value) => new Date(value).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}
                />
                <YAxis tickLine={false} axisLine={false} tickMargin={8} />
                <ChartTooltip
                  content={<ChartTooltipContent />}
                  labelFormatter={(value) => new Date(value).toLocaleDateString()}
                />
                <ChartLegend content={<ChartLegendContent />} />
                <Line
                  type="monotone"
                  dataKey="total_calls"
                  stroke="var(--color-total_calls)"
                  strokeWidth={2}
                  dot={false}
                />
                <Line
                  type="monotone"
                  dataKey="billable_calls"
                  stroke="var(--color-billable_calls)"
                  strokeWidth={2}
                  dot={false}
                />
              </LineChart>
            </ChartContainer>
          </CardContent>
        </Card>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
          {/* Endpoint Usage Bar Chart */}
          <Card>
            <CardHeader>
              <CardTitle>Calls by Endpoint</CardTitle>
              <CardDescription>Total calls per endpoint</CardDescription>
            </CardHeader>
            <CardContent>
              <ChartContainer config={endpointChartConfig} className="min-h-[300px] w-full">
                <BarChart data={endpointUsage} accessibilityLayer>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} />
                  <XAxis 
                    dataKey="endpoint" 
                    tickLine={false}
                    axisLine={false}
                    tickMargin={8}
                  />
                  <YAxis tickLine={false} axisLine={false} tickMargin={8} />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  <ChartLegend content={<ChartLegendContent />} />
                  <Bar dataKey="total_calls" fill="var(--color-total_calls)" radius={[4, 4, 0, 0]} />
                  <Bar dataKey="billable_calls" fill="var(--color-billable_calls)" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ChartContainer>
            </CardContent>
          </Card>

          {/* Endpoint Distribution Pie Chart */}
          <Card>
            <CardHeader>
              <CardTitle>Endpoint Distribution</CardTitle>
              <CardDescription>Percentage of calls by endpoint</CardDescription>
            </CardHeader>
            <CardContent>
              <ChartContainer config={endpointChartConfig} className="min-h-[300px] w-full">
                <PieChart>
                  <ChartTooltip content={<ChartTooltipContent hideLabel />} />
                  <Pie
                    data={endpointUsage as unknown as Record<string, unknown>[]}
                    dataKey="total_calls"
                    nameKey="endpoint"
                    cx="50%"
                    cy="50%"
                    outerRadius={100}
                    label
                  >
                    {endpointUsage.map((_, index) => (
                      <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                    ))}
                  </Pie>
                </PieChart>
              </ChartContainer>
            </CardContent>
          </Card>

          {/* Response Time Chart */}
          <Card>
            <CardHeader>
              <CardTitle>Average Response Time</CardTitle>
              <CardDescription>Performance by endpoint (ms)</CardDescription>
            </CardHeader>
            <CardContent>
              <ChartContainer config={responseTimeConfig} className="min-h-[300px] w-full">
                <BarChart data={endpointUsage} accessibilityLayer>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} />
                  <XAxis 
                    dataKey="endpoint" 
                    tickLine={false}
                    axisLine={false}
                    tickMargin={8}
                  />
                  <YAxis tickLine={false} axisLine={false} tickMargin={8} />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  <Bar dataKey="avg_response_time" fill="var(--color-avg_response_time)" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ChartContainer>
            </CardContent>
          </Card>

          {/* Success/Error Rates */}
          <Card>
            <CardHeader>
              <CardTitle>Success vs Error Rates</CardTitle>
              <CardDescription>Call outcomes by endpoint</CardDescription>
            </CardHeader>
            <CardContent>
              <ChartContainer config={successErrorConfig} className="min-h-[300px] w-full">
                <BarChart data={endpointUsage} accessibilityLayer>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} />
                  <XAxis 
                    dataKey="endpoint" 
                    tickLine={false}
                    axisLine={false}
                    tickMargin={8}
                  />
                  <YAxis tickLine={false} axisLine={false} tickMargin={8} />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  <ChartLegend content={<ChartLegendContent />} />
                  <Bar dataKey="success_count" fill="var(--color-success_count)" stackId="a" radius={[4, 4, 0, 0]} />
                  <Bar dataKey="error_count" fill="var(--color-error_count)" stackId="a" radius={[0, 0, 0, 0]} />
                </BarChart>
              </ChartContainer>
            </CardContent>
          </Card>
        </div>
      </main>
      <Toaster />
    </div>
  )
}
