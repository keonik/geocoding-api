import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { adminAPI, type AdminStats, type AdminUser, type AdminAPIKey, type UserUsageMetrics } from '@/api/admin'
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
} from 'lucide-react'
import { ThemeToggle } from '@/components/theme-toggle'

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
  const [metricsLoading, setMetricsLoading] = useState(false)

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
