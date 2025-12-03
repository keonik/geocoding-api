import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { adminAPI, type AdminStats, type AdminUser, type AdminAPIKey, type UserUsageMetrics, type AdminAnalytics } from '@/api/admin'
import { authAPI } from '@/api/auth'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Toaster } from '@/components/ui/toaster'
import { 
  Users, 
  Key, 
  Activity, 
  Database, 
  LogOut, 
  RefreshCw,
  ShieldCheck,
  ShieldOff,
  CheckCircle,
  XCircle,
  BarChart3,
  TrendingUp,
  Clock,
  CheckCircle2,
} from 'lucide-react'
import { ThemeToggle } from '@/components/theme-toggle'
import { LineChart, Line, BarChart, Bar, PieChart, Pie, Cell, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts'

export const Route = createFileRoute('/admin')({
  beforeLoad: () => {
    const token = localStorage.getItem('authToken')
    const user = JSON.parse(localStorage.getItem('user') || '{}')
    
    if (!token) {
      throw redirect({ to: '/auth/signin' })
    }
    
    if (!user.is_admin) {
      throw redirect({ to: '/dashboard' })
    }
  },
  component: AdminDashboard,
})

function AdminDashboard() {
  const navigate = useNavigate()
  const [stats, setStats] = useState<AdminStats | null>(null)
  const [users, setUsers] = useState<AdminUser[]>([])
  const [apiKeys, setAPIKeys] = useState<AdminAPIKey[]>([])
  const [loading, setLoading] = useState(true)
  const [activeTab, setActiveTab] = useState('users')
  const [selectedUserMetrics, setSelectedUserMetrics] = useState<UserUsageMetrics | null>(null)
  const [_metricsLoading, setMetricsLoading] = useState(false)
  const [analytics, setAnalytics] = useState<AdminAnalytics | null>(null)
  const [days, setDays] = useState(30)

  const user = JSON.parse(localStorage.getItem('user') || '{}')

  useEffect(() => {
    loadData()
  }, [])

  const loadData = async () => {
    try {
      setLoading(true)
      const [statsResponse, usersResponse, keysResponse] = await Promise.all([
        adminAPI.getStats(),
        adminAPI.getUsers(),
        adminAPI.getAPIKeys(),
      ])

      if (statsResponse.success && statsResponse.data) {
        setStats(statsResponse.data)
      }

      if (usersResponse.success && usersResponse.data) {
        setUsers(usersResponse.data)
      }

      if (keysResponse.success && keysResponse.data) {
        setAPIKeys(keysResponse.data)
      }
    } catch (err) {
      console.error('Error loading admin data:', err)
      toast.error('Failed to load admin data')
    } finally {
      setLoading(false)
    }
  }

  const handleToggleUserStatus = async (userId: number, currentStatus: boolean) => {
    if (!confirm(`Are you sure you want to ${currentStatus ? 'deactivate' : 'activate'} this user?`)) {
      return
    }

    try {
      await adminAPI.updateUserStatus(userId, !currentStatus)
      toast.success('User status updated successfully')
      loadData()
    } catch (err) {
      toast.error('Failed to update user status')
    }
  }

  const handleToggleUserAdmin = async (userId: number, currentAdmin: boolean) => {
    if (!confirm(`Are you sure you want to ${currentAdmin ? 'remove admin from' : 'make admin'} this user?`)) {
      return
    }

    try {
      await adminAPI.updateUserAdmin(userId, !currentAdmin)
      toast.success('Admin privileges updated successfully')
      loadData()
    } catch (err) {
      toast.error('Failed to update admin privileges')
    }
  }

  const handleLoadData = async () => {
    if (!confirm('This will reload all ZIP code data. This may take several minutes. Continue?')) {
      return
    }

    try {
      toast.info('Loading ZIP code data... This may take a few minutes.')
      await adminAPI.loadData()
      toast.success('ZIP code data loaded successfully')
      loadData()
    } catch (err) {
      toast.error('Failed to load ZIP code data')
    }
  }

  const handleLogout = () => {
    authAPI.logout()
    navigate({ to: '/' })
  }

  const handleViewMetrics = async (userId: number) => {
    try {
      setMetricsLoading(true)
      const response = await adminAPI.getUserMetrics(userId, 30)
      if (response.success && response.data) {
        setSelectedUserMetrics(response.data)
      } else {
        toast.error('Failed to load user metrics')
      }
    } catch (err) {
      toast.error('Failed to load user metrics')
    } finally {
      setMetricsLoading(false)
    }
  }

  const loadAnalytics = async () => {
    try {
      const response = await adminAPI.getAnalytics(days)
      if (response.success && response.data) {
        setAnalytics(response.data)
      }
    } catch (err) {
      console.error('Error loading analytics:', err)
      toast.error('Failed to load analytics data')
    }
  }

  useEffect(() => {
    if (activeTab === 'analytics') {
      loadAnalytics()
    }
  }, [activeTab, days])

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-lg">Loading...</div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="bg-card border-b">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4 flex justify-between items-center">
          <div>
            <h1 className="text-2xl font-bold">Admin Dashboard</h1>
            <p className="text-sm text-muted-foreground">{user.email}</p>
          </div>
          <div className="flex items-center space-x-4">
            <Button variant="outline" onClick={() => navigate({ to: '/data-manager' })}>
              <Database className="mr-2 h-4 w-4" />
              Data Manager
            </Button>
            <a
              href="/docs"
              target="_blank"
              rel="noopener noreferrer"
              className="text-muted-foreground hover:text-primary px-3 py-2 rounded-md text-sm font-medium"
            >
              Documentation
            </a>
            <ThemeToggle />
            <Button variant="outline" onClick={handleLogout}>
              <LogOut className="mr-2 h-4 w-4" />
              Logout
            </Button>
          </div>
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {/* Stats */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Users</CardTitle>
              <Users className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.total_users || 0}</div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Active API Keys</CardTitle>
              <Key className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.active_keys || 0}</div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">API Calls Today</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.calls_today || 0}</div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">ZIP Codes</CardTitle>
              <Database className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.zip_codes || 0}</div>
            </CardContent>
          </Card>
        </div>

        {/* Tabs */}
        <Card>
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <CardHeader>
              <TabsList>
                <TabsTrigger value="users">
                  <Users className="mr-2 h-4 w-4" />
                  Users
                </TabsTrigger>
                <TabsTrigger value="api-keys">
                  <Key className="mr-2 h-4 w-4" />
                  API Keys
                </TabsTrigger>
                <TabsTrigger value="analytics">
                  <BarChart3 className="mr-2 h-4 w-4" />
                  Analytics
                </TabsTrigger>
                <TabsTrigger value="system">
                  <Database className="mr-2 h-4 w-4" />
                  System
                </TabsTrigger>
              </TabsList>
            </CardHeader>

            <CardContent>
              {/* Users Tab */}
              <TabsContent value="users" className="space-y-4">
                <div className="flex justify-between items-center">
                  <h3 className="text-lg font-medium">User Management</h3>
                  <Button variant="outline" size="sm" onClick={loadData}>
                    <RefreshCw className="mr-2 h-4 w-4" />
                    Refresh
                  </Button>
                </div>

                <div className="rounded-md border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>User</TableHead>
                        <TableHead>Plan</TableHead>
                        <TableHead>Usage</TableHead>
                        <TableHead>Status</TableHead>
                        <TableHead>Created</TableHead>
                        <TableHead>Actions</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {users.map((u) => (
                        <TableRow key={u.id}>
                          <TableCell>
                            <div>
                              <div className="font-medium">{u.name}</div>
                              <div className="text-sm text-muted-foreground">{u.email}</div>
                            </div>
                          </TableCell>
                          <TableCell>
                            <Badge variant="outline">{u.plan_type}</Badge>
                          </TableCell>
                          <TableCell>
                            <div className="text-sm space-y-1">
                              <div className="flex items-center gap-2">
                                <TrendingUp className="h-3 w-3 text-muted-foreground" />
                                <span className="font-medium">{u.monthly_usage || 0}</span>
                                <span className="text-muted-foreground">/ month</span>
                              </div>
                              <div className="text-xs text-muted-foreground">
                                Today: {u.today_usage || 0} | Total: {u.total_usage || 0}
                              </div>
                              <div className="text-xs text-muted-foreground">
                                Keys: {u.active_keys || 0}
                              </div>
                            </div>
                          </TableCell>
                          <TableCell>
                            <div className="flex gap-2">
                              {u.is_active ? (
                                <Badge variant="default">Active</Badge>
                              ) : (
                                <Badge variant="destructive">Inactive</Badge>
                              )}
                              {u.is_admin && <Badge variant="secondary">Admin</Badge>}
                            </div>
                          </TableCell>
                          <TableCell>
                            {new Date(u.created_at).toLocaleDateString()}
                          </TableCell>
                          <TableCell>
                            <div className="flex gap-2 flex-wrap">
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handleViewMetrics(u.id)}
                              >
                                <BarChart3 className="mr-1 h-3 w-3" />
                                Metrics
                              </Button>
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handleToggleUserStatus(u.id, u.is_active)}
                              >
                                {u.is_active ? (
                                  <>
                                    <XCircle className="mr-1 h-3 w-3" />
                                    Deactivate
                                  </>
                                ) : (
                                  <>
                                    <CheckCircle className="mr-1 h-3 w-3" />
                                    Activate
                                  </>
                                )}
                              </Button>
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handleToggleUserAdmin(u.id, u.is_admin)}
                              >
                                {u.is_admin ? (
                                  <>
                                    <ShieldOff className="mr-1 h-3 w-3" />
                                    Remove Admin
                                  </>
                                ) : (
                                  <>
                                    <ShieldCheck className="mr-1 h-3 w-3" />
                                    Make Admin
                                  </>
                                )}
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </TabsContent>

              {/* API Keys Tab */}
              <TabsContent value="api-keys" className="space-y-4">
                <div className="flex justify-between items-center">
                  <h3 className="text-lg font-medium">API Key Management</h3>
                  <Button variant="outline" size="sm" onClick={loadData}>
                    <RefreshCw className="mr-2 h-4 w-4" />
                    Refresh
                  </Button>
                </div>

                <div className="rounded-md border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Key Preview</TableHead>
                        <TableHead>User</TableHead>
                        <TableHead>Name</TableHead>
                        <TableHead>Last Used</TableHead>
                        <TableHead>Status</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {apiKeys.map((key) => (
                        <TableRow key={key.id}>
                          <TableCell>
                            <code className="text-sm bg-muted px-2 py-1 rounded">
                              {key.key_preview}
                            </code>
                          </TableCell>
                          <TableCell>{key.user_email}</TableCell>
                          <TableCell>{key.name}</TableCell>
                          <TableCell>
                            {key.last_used_at
                              ? new Date(key.last_used_at).toLocaleDateString()
                              : 'Never'}
                          </TableCell>
                          <TableCell>
                            {key.is_active ? (
                              <Badge variant="default">Active</Badge>
                            ) : (
                              <Badge variant="destructive">Inactive</Badge>
                            )}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </TabsContent>

              {/* Analytics Tab */}
              <TabsContent value="analytics" className="space-y-4">
                <div className="flex justify-between items-center mb-4">
                  <h3 className="text-lg font-medium">System-Wide Analytics</h3>
                  <div className="flex gap-2">
                    <select 
                      value={days} 
                      onChange={(e) => setDays(Number(e.target.value))}
                      className="px-3 py-1 border rounded-md"
                    >
                      <option value={7}>Last 7 days</option>
                      <option value={30}>Last 30 days</option>
                      <option value={90}>Last 90 days</option>
                    </select>
                    <Button variant="outline" size="sm" onClick={loadAnalytics}>
                      <RefreshCw className="mr-2 h-4 w-4" />
                      Refresh
                    </Button>
                  </div>
                </div>

                {analytics && (
                  <>
                    {/* Summary Stats */}
                    <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
                      <Card>
                        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                          <CardTitle className="text-sm font-medium">Total Calls</CardTitle>
                          <Activity className="h-4 w-4 text-muted-foreground" />
                        </CardHeader>
                        <CardContent>
                          <div className="text-2xl font-bold">{analytics.total_calls.toLocaleString()}</div>
                          <p className="text-xs text-muted-foreground">Last {days} days</p>
                        </CardContent>
                      </Card>

                      <Card>
                        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                          <CardTitle className="text-sm font-medium">Billable Calls</CardTitle>
                          <TrendingUp className="h-4 w-4 text-muted-foreground" />
                        </CardHeader>
                        <CardContent>
                          <div className="text-2xl font-bold">{analytics.billable_calls.toLocaleString()}</div>
                          <p className="text-xs text-muted-foreground">
                            {analytics.total_calls > 0 ? Math.round((analytics.billable_calls / analytics.total_calls) * 100) : 0}% of total
                          </p>
                        </CardContent>
                      </Card>

                      <Card>
                        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                          <CardTitle className="text-sm font-medium">Avg Response Time</CardTitle>
                          <Clock className="h-4 w-4 text-muted-foreground" />
                        </CardHeader>
                        <CardContent>
                          <div className="text-2xl font-bold">{analytics.avg_response_time.toFixed(0)}ms</div>
                          <p className="text-xs text-muted-foreground">Average across all calls</p>
                        </CardContent>
                      </Card>

                      <Card>
                        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                          <CardTitle className="text-sm font-medium">Success Rate</CardTitle>
                          <CheckCircle2 className="h-4 w-4 text-muted-foreground" />
                        </CardHeader>
                        <CardContent>
                          <div className="text-2xl font-bold">
                            {analytics.total_calls > 0 
                              ? Math.round((analytics.success_count / analytics.total_calls) * 100) 
                              : 0}%
                          </div>
                          <p className="text-xs text-muted-foreground">
                            {analytics.success_count.toLocaleString()} successful
                          </p>
                        </CardContent>
                      </Card>
                    </div>

                    {/* Daily Usage Chart */}
                    <Card>
                      <CardHeader>
                        <CardTitle>Daily Usage</CardTitle>
                        <CardDescription>API calls over time (system-wide)</CardDescription>
                      </CardHeader>
                      <CardContent>
                        <ResponsiveContainer width="100%" height={300}>
                          <LineChart data={analytics.daily_usage}>
                            <CartesianGrid strokeDasharray="3 3" />
                            <XAxis dataKey="date" />
                            <YAxis />
                            <Tooltip />
                            <Legend />
                            <Line 
                              type="monotone" 
                              dataKey="total_calls" 
                              stroke="hsl(var(--primary))" 
                              name="Total Calls"
                            />
                            <Line 
                              type="monotone" 
                              dataKey="billable_calls" 
                              stroke="hsl(var(--chart-2))" 
                              name="Billable Calls"
                            />
                          </LineChart>
                        </ResponsiveContainer>
                      </CardContent>
                    </Card>

                    {/* Endpoint Usage Charts */}
                    <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                      <Card>
                        <CardHeader>
                          <CardTitle>Endpoint Usage</CardTitle>
                          <CardDescription>Total calls per endpoint</CardDescription>
                        </CardHeader>
                        <CardContent>
                          <ResponsiveContainer width="100%" height={300}>
                            <BarChart data={analytics.endpoints}>
                              <CartesianGrid strokeDasharray="3 3" />
                              <XAxis dataKey="endpoint" angle={-45} textAnchor="end" height={100} />
                              <YAxis />
                              <Tooltip />
                              <Bar dataKey="total" fill="hsl(var(--primary))" name="Total Calls" />
                            </BarChart>
                          </ResponsiveContainer>
                        </CardContent>
                      </Card>

                      <Card>
                        <CardHeader>
                          <CardTitle>Response Times</CardTitle>
                          <CardDescription>Average response time by endpoint</CardDescription>
                        </CardHeader>
                        <CardContent>
                          <ResponsiveContainer width="100%" height={300}>
                            <BarChart data={analytics.endpoints}>
                              <CartesianGrid strokeDasharray="3 3" />
                              <XAxis dataKey="endpoint" angle={-45} textAnchor="end" height={100} />
                              <YAxis />
                              <Tooltip />
                              <Bar dataKey="avg_time" fill="hsl(var(--chart-3))" name="Avg Time (ms)" />
                            </BarChart>
                          </ResponsiveContainer>
                        </CardContent>
                      </Card>
                    </div>

                    {/* Success/Error Distribution */}
                    <Card>
                      <CardHeader>
                        <CardTitle>Success vs Errors</CardTitle>
                        <CardDescription>Distribution of successful and failed requests</CardDescription>
                      </CardHeader>
                      <CardContent>
                        <ResponsiveContainer width="100%" height={300}>
                          <PieChart>
                            <Pie
                              data={[
                                { name: 'Success', value: analytics.success_count, color: 'hsl(var(--chart-1))' },
                                { name: 'Errors', value: analytics.error_count, color: 'hsl(var(--chart-5))' }
                              ]}
                              cx="50%"
                              cy="50%"
                              labelLine={false}
                              label={({ name, percent }) => `${name}: ${((percent ?? 0) * 100).toFixed(0)}%`}
                              outerRadius={80}
                              fill="#8884d8"
                              dataKey="value"
                            >
                              <Cell fill="hsl(var(--chart-1))" />
                              <Cell fill="hsl(var(--chart-5))" />
                            </Pie>
                            <Tooltip />
                          </PieChart>
                        </ResponsiveContainer>
                      </CardContent>
                    </Card>
                  </>
                )}

                {!analytics && (
                  <Card>
                    <CardContent className="py-10 text-center text-muted-foreground">
                      Loading analytics...
                    </CardContent>
                  </Card>
                )}
              </TabsContent>

              {/* System Tab */}
              <TabsContent value="system" className="space-y-4">
                <div className="grid gap-4">
                  <Card>
                    <CardHeader>
                      <CardTitle>Data Management</CardTitle>
                      <CardDescription>
                        Manage system data and perform maintenance tasks
                      </CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-2">
                      <Button onClick={handleLoadData} className="w-full">
                        <Database className="mr-2 h-4 w-4" />
                        Reload ZIP Code Data
                      </Button>
                    </CardContent>
                  </Card>
                </div>
              </TabsContent>
            </CardContent>
          </Tabs>
        </Card>

        {/* User Metrics Modal */}
        {selectedUserMetrics && (
          <Card className="mt-6">
            <CardHeader>
              <div className="flex justify-between items-center">
                <div>
                  <CardTitle>User Metrics: {selectedUserMetrics.email}</CardTitle>
                  <CardDescription>
                    Detailed usage analytics for the past 30 days
                  </CardDescription>
                </div>
                <Button 
                  variant="outline" 
                  size="sm"
                  onClick={() => setSelectedUserMetrics(null)}
                >
                  Close
                </Button>
              </div>
            </CardHeader>
            <CardContent className="space-y-6">
              {/* Summary Stats */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <Card>
                  <CardHeader className="pb-2">
                    <CardDescription>Total Calls</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <div className="text-2xl font-bold">{selectedUserMetrics.total_calls}</div>
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2">
                    <CardDescription>Billable Calls</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <div className="text-2xl font-bold">{selectedUserMetrics.billable_calls}</div>
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2">
                    <CardDescription>Avg Response</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <div className="text-2xl font-bold">
                      {selectedUserMetrics.avg_response_time.toFixed(0)}ms
                    </div>
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2">
                    <CardDescription>Success Rate</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <div className="text-2xl font-bold">
                      {((selectedUserMetrics.success_count / (selectedUserMetrics.success_count + selectedUserMetrics.error_count)) * 100).toFixed(1)}%
                    </div>
                  </CardContent>
                </Card>
              </div>

              {/* Endpoint Breakdown */}
              <div>
                <h3 className="text-lg font-medium mb-3">Endpoint Usage</h3>
                <div className="rounded-md border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Endpoint</TableHead>
                        <TableHead>Total Calls</TableHead>
                        <TableHead>Billable</TableHead>
                        <TableHead>Avg Response</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {selectedUserMetrics.endpoints.map((endpoint, idx) => (
                        <TableRow key={idx}>
                          <TableCell>
                            <code className="text-sm">{endpoint.endpoint}</code>
                          </TableCell>
                          <TableCell>{endpoint.total}</TableCell>
                          <TableCell>{endpoint.billable}</TableCell>
                          <TableCell>{endpoint.avg_time.toFixed(0)}ms</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </div>

              {/* Daily Usage Chart */}
              <div>
                <h3 className="text-lg font-medium mb-3">Daily Usage (Last 30 Days)</h3>
                <div className="rounded-md border p-4">
                  <div className="space-y-2">
                    {selectedUserMetrics.daily_usage.slice(0, 10).map((day, idx) => (
                      <div key={idx} className="flex justify-between items-center">
                        <span className="text-sm text-muted-foreground">{day.date}</span>
                        <div className="flex gap-4">
                          <span className="text-sm">
                            Total: <span className="font-medium">{day.total}</span>
                          </span>
                          <span className="text-sm">
                            Billable: <span className="font-medium">{day.billable}</span>
                          </span>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>
        )}
      </main>
      <Toaster />
    </div>
  )
}
