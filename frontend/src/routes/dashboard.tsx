import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { apiKeysAPI } from '@/api/apiKeys'
import { usageAPI } from '@/api/usage'
import { authAPI } from '@/api/auth'
import type { APIKey, UsageStats } from '@/types/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { 
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Toaster } from '@/components/ui/toaster'
import { Key, Trash2, Copy, Plus, LogOut } from 'lucide-react'
import { ThemeToggle } from '@/components/theme-toggle'

export const Route = createFileRoute('/dashboard')({
  beforeLoad: () => {
    const token = localStorage.getItem('authToken')
    if (!token) {
      throw redirect({ to: '/auth/signin' })
    }
  },
  component: Dashboard,
})

function Dashboard() {
  const navigate = useNavigate()
  const [apiKeys, setApiKeys] = useState<APIKey[]>([])
  const [usage, setUsage] = useState<UsageStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [createModalOpen, setCreateModalOpen] = useState(false)
  const [showKeyModal, setShowKeyModal] = useState(false)
  const [newKeyString, setNewKeyString] = useState('')
  const [keyName, setKeyName] = useState('')
  const [selectedPermissions, setSelectedPermissions] = useState<string[]>(['*'])
  const [error, setError] = useState('')

  const user = JSON.parse(localStorage.getItem('user') || '{}')

  useEffect(() => {
    loadData()
  }, [])

  const loadData = async () => {
    try {
      setLoading(true)
      const [keysResponse, usageResponse] = await Promise.all([
        apiKeysAPI.list(),
        usageAPI.getStats(),
      ])

      if (keysResponse.success && keysResponse.data) {
        setApiKeys(keysResponse.data.api_keys || [])
      }

      if (usageResponse.success && usageResponse.data) {
        setUsage(usageResponse.data)
      }
    } catch (err) {
      console.error('Error loading dashboard data:', err)
      setError('Failed to load dashboard data')
    } finally {
      setLoading(false)
    }
  }

  const handleCreateKey = async () => {
    if (!keyName.trim()) {
      setError('Please enter a key name')
      return
    }

    try {
      const response = await apiKeysAPI.create({
        name: keyName,
        permissions: selectedPermissions,
      })

      if (response.success && response.data) {
        setNewKeyString(response.data.key_string)
        setCreateModalOpen(false)
        setShowKeyModal(true)
        setKeyName('')
        setSelectedPermissions(['*'])
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create API key')
    }
  }

  const handleDeleteKey = async (keyId: string) => {
    if (!confirm('Are you sure you want to delete this API key?')) {
      return
    }

    try {
      await apiKeysAPI.delete(keyId)
      loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete API key')
    }
  }

  const handleCopyKey = () => {
    navigator.clipboard.writeText(newKeyString)
    toast.success('API key copied to clipboard!')
  }

  const handleLogout = () => {
    authAPI.logout()
    navigate({ to: '/' })
  }

  const togglePermission = (perm: string) => {
    if (perm === '*') {
      setSelectedPermissions(['*'])
    } else {
      setSelectedPermissions(prev => {
        const filtered = prev.filter(p => p !== '*')
        if (prev.includes(perm)) {
          return filtered.filter(p => p !== perm)
        }
        return [...filtered, perm]
      })
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
            <h1 className="text-2xl font-bold">GeoCode API Dashboard</h1>
            <p className="text-sm text-muted-foreground">{user.email}</p>
          </div>
          <div className="flex items-center space-x-4">
            <a href="/docs" target="_blank" rel="noopener noreferrer" className="text-muted-foreground hover:text-primary px-3 py-2 rounded-md text-sm font-medium">
              Documentation
            </a>
            {user.is_admin && (
              <Button variant="outline" onClick={() => navigate({ to: '/admin' })}>
                Admin
              </Button>
            )}
            <ThemeToggle />
            <Button variant="outline" onClick={handleLogout}>
              <LogOut className="mr-2 h-4 w-4" />
              Logout
            </Button>
          </div>
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {error && (
          <div className="mb-4 bg-destructive/10 text-destructive p-4 rounded-md">
            {error}
          </div>
        )}

        {/* Stats */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-8">
          <Card className="cursor-pointer hover:bg-accent" onClick={() => navigate({ to: '/usage' })}>
            <CardHeader>
              <CardDescription>Current Usage</CardDescription>
              <CardTitle className="text-3xl">
                {usage?.rate_limit?.current_usage || 0}
              </CardTitle>
              <p className="text-xs text-muted-foreground mt-2">Click for detailed analytics →</p>
            </CardHeader>
          </Card>
          <Card>
            <CardHeader>
              <CardDescription>Monthly Limit</CardDescription>
              <CardTitle className="text-3xl">
                {usage?.rate_limit?.monthly_limit || 0}
              </CardTitle>
            </CardHeader>
          </Card>
          <Card>
            <CardHeader>
              <CardDescription>API Keys</CardDescription>
              <CardTitle className="text-3xl">{apiKeys.length}</CardTitle>
            </CardHeader>
          </Card>
        </div>

        {/* API Keys Section */}
        <Card>
          <CardHeader>
            <div className="flex justify-between items-center">
              <div>
                <CardTitle>API Keys</CardTitle>
                <CardDescription>Manage your API keys</CardDescription>
              </div>
              <Button onClick={() => setCreateModalOpen(true)}>
                <Plus className="mr-2 h-4 w-4" />
                Create API Key
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            {apiKeys.length === 0 ? (
              <div className="text-center py-12">
                <Key className="mx-auto h-12 w-12 text-muted-foreground mb-4" />
                <h3 className="text-lg font-medium mb-2">No API Keys Yet</h3>
                <p className="text-muted-foreground mb-4">
                  Create your first API key to start using the GeoCode API
                </p>
                <Button onClick={() => setCreateModalOpen(true)}>
                  <Plus className="mr-2 h-4 w-4" />
                  Create Your First Key
                </Button>
              </div>
            ) : (
              <div className="space-y-4">
                {apiKeys.map((key) => (
                  <div
                    key={key.id}
                    className="flex items-center justify-between p-4 border rounded-lg hover:bg-accent"
                  >
                    <div className="flex-1">
                      <h4 className="font-medium">{key.name}</h4>
                      <p className="text-sm text-muted-foreground font-mono">
                        {key.key_preview}
                      </p>
                      <div className="mt-2 flex flex-wrap gap-1">
                        {key.permissions.map((perm) => (
                          <span
                            key={perm}
                            className="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-blue-100 text-blue-800"
                          >
                            {perm === '*' ? 'All permissions' : perm}
                          </span>
                        ))}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        Created {new Date(key.created_at).toLocaleDateString()}
                        {key.last_used_at && (
                          <> • Last used {new Date(key.last_used_at).toLocaleDateString()}</>
                        )}
                      </div>
                    </div>
                    <Button
                      variant="destructive"
                      size="icon"
                      onClick={() => handleDeleteKey(key.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </main>

      {/* Create Key Modal */}
      <Dialog open={createModalOpen} onOpenChange={setCreateModalOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create API Key</DialogTitle>
            <DialogDescription>
              Create a new API key with specific permissions
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="keyName">Key Name</Label>
              <Input
                id="keyName"
                value={keyName}
                onChange={(e) => setKeyName(e.target.value)}
                placeholder="My API Key"
              />
            </div>
            <div className="space-y-2">
              <Label>Permissions</Label>
              <div className="space-y-2">
                {['*', 'geocode', 'reverse_geocode', 'batch_geocode'].map((perm) => (
                  <div key={perm} className="flex items-center">
                    <input
                      type="checkbox"
                      id={perm}
                      checked={selectedPermissions.includes(perm)}
                      onChange={() => togglePermission(perm)}
                      className="mr-2"
                    />
                    <Label htmlFor={perm} className="cursor-pointer">
                      {perm === '*' ? 'All permissions' : perm}
                    </Label>
                  </div>
                ))}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateModalOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleCreateKey}>Create Key</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Show New Key Modal */}
      <Dialog open={showKeyModal} onOpenChange={setShowKeyModal}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>API Key Created</DialogTitle>
            <DialogDescription>
              Save this key now - you won't be able to see it again!
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>Your API Key</Label>
              <div className="flex gap-2">
                <Input value={newKeyString} readOnly />
                <Button variant="outline" size="icon" onClick={handleCopyKey}>
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => { setShowKeyModal(false); loadData(); }}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Toaster />
    </div>
  )
}
