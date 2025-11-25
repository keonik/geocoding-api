import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useEffect, useState, useRef } from 'react'
import { toast } from 'sonner'
import { datasetAPI, type Dataset, type DatasetStats } from '@/api/datasets'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { ThemeToggle } from '@/components/theme-toggle'
import { 
  Upload, 
  FileText, 
  Trash2, 
  RefreshCw, 
  Database, 
  HardDrive,
  MapPin,
  ArrowLeft,
  CheckCircle,
  XCircle,
  Clock,
  Loader2
} from 'lucide-react'
import { Toaster } from '@/components/ui/toaster'

export const Route = createFileRoute('/data-manager')({
  beforeLoad: () => {
    const token = localStorage.getItem('authToken')
    const user = JSON.parse(localStorage.getItem('user') || '{}')
    if (!token || !user.is_admin) {
      throw redirect({ to: '/dashboard' })
    }
  },
  component: DataManager,
})

function DataManager() {
  const navigate = useNavigate()
  const [datasets, setDatasets] = useState<Dataset[]>([])
  const [stats, setStats] = useState<DatasetStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [uploadModalOpen, setUploadModalOpen] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [uploadProgress, setUploadProgress] = useState(0)
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [stateFilter, setStateFilter] = useState<string>('')
  
  // Upload form state
  const [uploadForm, setUploadForm] = useState({
    name: '',
    state: '',
    county: '',
    file: null as File | null,
  })
  
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    loadData()
    // Poll for dataset status updates every 5 seconds
    const interval = setInterval(loadData, 5000)
    return () => clearInterval(interval)
  }, [statusFilter, stateFilter])

  const loadData = async () => {
    try {
      const [datasetsResponse, statsResponse] = await Promise.all([
        datasetAPI.list({ 
          status: statusFilter || undefined,
          state: stateFilter || undefined,
        }),
        datasetAPI.getStats(),
      ])

      if (datasetsResponse.success && datasetsResponse.data) {
        setDatasets(datasetsResponse.data.datasets)
      }

      if (statsResponse.success && statsResponse.data) {
        setStats(statsResponse.data)
      }
    } catch (err) {
      console.error('Error loading datasets:', err)
      toast.error('Failed to load datasets')
    } finally {
      setLoading(false)
    }
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files[0]) {
      setUploadForm({ ...uploadForm, file: e.target.files[0] })
    }
  }

  const handleUpload = async () => {
    if (!uploadForm.name || !uploadForm.state || !uploadForm.county || !uploadForm.file) {
      toast.error('Please fill in all fields and select a file')
      return
    }

    try {
      setUploading(true)
      setUploadProgress(0)

      const formData = new FormData()
      formData.append('name', uploadForm.name)
      formData.append('state', uploadForm.state)
      formData.append('county', uploadForm.county)
      formData.append('file', uploadForm.file)

      // Simulate progress (real progress would need xhr or fetch with progress events)
      const progressInterval = setInterval(() => {
        setUploadProgress(prev => Math.min(prev + 10, 90))
      }, 200)

      const response = await datasetAPI.upload(formData)

      clearInterval(progressInterval)
      setUploadProgress(100)

      if (response.success) {
        toast.success('File uploaded successfully! Processing started.')
        setUploadModalOpen(false)
        setUploadForm({ name: '', state: '', county: '', file: null })
        if (fileInputRef.current) fileInputRef.current.value = ''
        loadData()
      }
    } catch (err) {
      toast.error('Failed to upload file')
    } finally {
      setUploading(false)
      setUploadProgress(0)
    }
  }

  const handleDelete = async (id: number) => {
    if (!confirm('Are you sure you want to delete this dataset?')) {
      return
    }

    try {
      await datasetAPI.delete(id)
      toast.success('Dataset deleted successfully')
      loadData()
    } catch (err) {
      toast.error('Failed to delete dataset')
    }
  }

  const handleReprocess = async (id: number) => {
    try {
      await datasetAPI.reprocess(id)
      toast.success('Reprocessing started')
      loadData()
    } catch (err) {
      toast.error('Failed to reprocess dataset')
    }
  }

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 Bytes'
    const k = 1024
    const sizes = ['Bytes', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i]
  }

  const formatDate = (date: string) => {
    return new Date(date).toLocaleString()
  }

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'completed':
        return <CheckCircle className="h-4 w-4 text-green-500" />
      case 'failed':
        return <XCircle className="h-4 w-4 text-red-500" />
      case 'processing':
        return <Loader2 className="h-4 w-4 text-blue-500 animate-spin" />
      default:
        return <Clock className="h-4 w-4 text-gray-500" />
    }
  }

  const getStatusBadge = (status: string) => {
    const variants: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
      completed: 'default',
      processing: 'secondary',
      failed: 'destructive',
      pending: 'outline',
    }
    return <Badge variant={variants[status] || 'outline'}>{status}</Badge>
  }

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin" />
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-background">
      <Toaster />
      
      {/* Header */}
      <header className="bg-card border-b">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4 flex justify-between items-center">
          <div>
            <h1 className="text-2xl font-bold">Data Manager</h1>
            <p className="text-sm text-muted-foreground">Upload and manage county address datasets</p>
          </div>
          <div className="flex items-center space-x-4">
            <Button variant="ghost" onClick={() => navigate({ to: '/admin' })}>
              <ArrowLeft className="mr-2 h-4 w-4" />
              Back to Admin
            </Button>
            <ThemeToggle />
          </div>
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Datasets</CardTitle>
              <Database className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.total_datasets || 0}</div>
            </CardContent>
          </Card>
          
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Records</CardTitle>
              <MapPin className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.total_records.toLocaleString() || 0}</div>
            </CardContent>
          </Card>
          
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Storage Used</CardTitle>
              <HardDrive className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatBytes(stats?.total_storage_size || 0)}</div>
            </CardContent>
          </Card>
          
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">States Covered</CardTitle>
              <FileText className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{Object.keys(stats?.state_breakdown || {}).length}</div>
            </CardContent>
          </Card>
        </div>

        {/* Upload Section */}
        <Card className="mb-8">
          <CardHeader>
            <CardTitle>Upload New Dataset</CardTitle>
            <CardDescription>
              Upload county address data in GeoJSON format (.geojson or .geojson.gz)
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button onClick={() => setUploadModalOpen(true)}>
              <Upload className="mr-2 h-4 w-4" />
              Upload Dataset
            </Button>
          </CardContent>
        </Card>

        {/* Datasets Table */}
        <Card>
          <CardHeader>
            <div className="flex justify-between items-center">
              <div>
                <CardTitle>Datasets</CardTitle>
                <CardDescription>Manage uploaded county address datasets</CardDescription>
              </div>
              <div className="flex space-x-2">
                <select
                  value={statusFilter}
                  onChange={(e) => setStatusFilter(e.target.value)}
                  className="px-3 py-2 border rounded-md"
                >
                  <option value="">All Status</option>
                  <option value="pending">Pending</option>
                  <option value="processing">Processing</option>
                  <option value="completed">Completed</option>
                  <option value="failed">Failed</option>
                </select>
                
                <Input
                  placeholder="Filter by state..."
                  value={stateFilter}
                  onChange={(e) => setStateFilter(e.target.value)}
                  className="w-32"
                />
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>State/County</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Records</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead>Uploaded</TableHead>
                  <TableHead>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {datasets.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} className="text-center text-muted-foreground">
                      No datasets uploaded yet
                    </TableCell>
                  </TableRow>
                ) : (
                  datasets.map((dataset) => (
                    <TableRow key={dataset.id}>
                      <TableCell className="font-medium">
                        <div className="flex items-center space-x-2">
                          {getStatusIcon(dataset.status)}
                          <span>{dataset.name}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        {dataset.state}, {dataset.county}
                      </TableCell>
                      <TableCell>{getStatusBadge(dataset.status)}</TableCell>
                      <TableCell>{dataset.record_count.toLocaleString()}</TableCell>
                      <TableCell>{formatBytes(dataset.file_size)}</TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDate(dataset.uploaded_at)}
                      </TableCell>
                      <TableCell>
                        <div className="flex space-x-2">
                          {dataset.status === 'failed' && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleReprocess(dataset.id)}
                            >
                              <RefreshCw className="h-4 w-4" />
                            </Button>
                          )}
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDelete(dataset.id)}
                          >
                            <Trash2 className="h-4 w-4 text-red-500" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </main>

      {/* Upload Modal */}
      <Dialog open={uploadModalOpen} onOpenChange={setUploadModalOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Upload Dataset</DialogTitle>
            <DialogDescription>
              Upload a county address dataset in GeoJSON format
            </DialogDescription>
          </DialogHeader>
          
          <div className="space-y-4">
            <div>
              <Label htmlFor="name">Dataset Name</Label>
              <Input
                id="name"
                placeholder="e.g., Adams County Addresses"
                value={uploadForm.name}
                onChange={(e) => setUploadForm({ ...uploadForm, name: e.target.value })}
              />
            </div>
            
            <div className="grid grid-cols-2 gap-4">
              <div>
                <Label htmlFor="state">State Code</Label>
                <Input
                  id="state"
                  placeholder="e.g., OH"
                  value={uploadForm.state}
                  onChange={(e) => setUploadForm({ ...uploadForm, state: e.target.value.toUpperCase() })}
                  maxLength={2}
                />
              </div>
              
              <div>
                <Label htmlFor="county">County Name</Label>
                <Input
                  id="county"
                  placeholder="e.g., Adams"
                  value={uploadForm.county}
                  onChange={(e) => setUploadForm({ ...uploadForm, county: e.target.value })}
                />
              </div>
            </div>
            
            <div>
              <Label htmlFor="file">File (.geojson or .geojson.gz)</Label>
              <Input
                id="file"
                type="file"
                accept=".geojson,.json,.gz"
                ref={fileInputRef}
                onChange={handleFileChange}
              />
              {uploadForm.file && (
                <p className="text-sm text-muted-foreground mt-2">
                  Selected: {uploadForm.file.name} ({formatBytes(uploadForm.file.size)})
                </p>
              )}
            </div>
            
            {uploading && (
              <div className="space-y-2">
                <div className="w-full bg-gray-200 rounded-full h-2">
                  <div
                    className="bg-blue-600 h-2 rounded-full transition-all duration-300"
                    style={{ width: `${uploadProgress}%` }}
                  />
                </div>
                <p className="text-sm text-center text-muted-foreground">
                  Uploading... {uploadProgress}%
                </p>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setUploadModalOpen(false)} disabled={uploading}>
              Cancel
            </Button>
            <Button onClick={handleUpload} disabled={uploading}>
              {uploading ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Upload className="mr-2 h-4 w-4" />}
              Upload
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
