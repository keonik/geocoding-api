import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useEffect, useState, useRef } from 'react'
import { toast } from 'sonner'
import { datasetAPI, type Dataset, type DatasetStats, type BatchUploadResult } from '@/api/datasets'
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
import { Progress } from '@/components/ui/progress'
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
  Loader2,
  Files
} from 'lucide-react'
import { Toaster } from '@/components/ui/toaster'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

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
  const [bulkUploadModalOpen, setBulkUploadModalOpen] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [uploadStatus, setUploadStatus] = useState<string>('')
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [stateFilter, setStateFilter] = useState<string>('')
  
  // Multi-select state for bulk delete
  const [selectedDatasets, setSelectedDatasets] = useState<Set<number>>(new Set())
  const [deletingMultiple, setDeletingMultiple] = useState(false)
  
  // Single upload form state
  const [uploadForm, setUploadForm] = useState({
    name: '',
    state: '',
    county: '',
    file: null as File | null,
  })
  
  // Bulk upload form state
  const [bulkUploadForm, setBulkUploadForm] = useState({
    state: '',
    files: [] as File[],
  })
  const [bulkUploadResults, setBulkUploadResults] = useState<BatchUploadResult[]>([])
  
  // Upload progress state
  const [uploadProgress, setUploadProgress] = useState({
    currentFile: '',
    currentIndex: 0,
    totalFiles: 0,
    completedCount: 0,
    failedCount: 0,
    processingMessage: '',
    // Byte-level progress for upload phase
    bytesLoaded: 0,
    bytesTotal: 0,
    percentComplete: 0,
    phase: 'idle' as 'idle' | 'uploading' | 'processing' | 'complete',
  })
  
  // Reference to abort upload
  const uploadAbortRef = useRef<{ abort: () => void } | null>(null)
  
  const fileInputRef = useRef<HTMLInputElement>(null)
  const bulkFileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    loadData()
    // Poll for dataset status updates every 5 seconds
    const interval = setInterval(loadData, 5000)
    return () => clearInterval(interval)
  }, [statusFilter, stateFilter])

  // Clear selection when datasets change (e.g., after filtering)
  useEffect(() => {
    setSelectedDatasets(new Set())
  }, [statusFilter, stateFilter])

  const loadData = async () => {
    try {
      const [datasetsResponse, statsResponse] = await Promise.all([
        datasetAPI.list({ 
          status: statusFilter && statusFilter !== 'all' ? statusFilter : undefined,
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

  const handleBulkFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) {
      setBulkUploadForm({ ...bulkUploadForm, files: Array.from(e.target.files) })
    }
  }

  const handleUpload = async () => {
    if (!uploadForm.name || !uploadForm.state || !uploadForm.county || !uploadForm.file) {
      toast.error('Please fill in all fields and select a file')
      return
    }

    try {
      setUploading(true)
      setUploadStatus('Uploading file...')

      const formData = new FormData()
      formData.append('name', uploadForm.name)
      formData.append('state', uploadForm.state)
      formData.append('county', uploadForm.county)
      formData.append('file', uploadForm.file)

      const response = await datasetAPI.upload(formData)

      if (response.success) {
        setUploadStatus('Upload complete! Processing started in background.')
        toast.success('File uploaded successfully! Processing started.')
        
        // Wait a moment to show success, then close
        setTimeout(() => {
          setUploadModalOpen(false)
          setUploadForm({ name: '', state: '', county: '', file: null })
          setUploadStatus('')
          if (fileInputRef.current) fileInputRef.current.value = ''
          loadData()
        }, 1500)
      } else {
        setUploadStatus('Upload failed')
        toast.error(response.error || 'Upload failed')
      }
    } catch (err: unknown) {
      const errorMessage = err instanceof Error ? err.message : 'Failed to upload file'
      setUploadStatus(`Error: ${errorMessage}`)
      toast.error(errorMessage)
    } finally {
      setUploading(false)
    }
  }

  const handleBulkUpload = () => {
    if (!bulkUploadForm.state || bulkUploadForm.files.length === 0) {
      toast.error('Please select a state and at least one file')
      return
    }

    setUploading(true)
    setBulkUploadResults([])
    
    // Calculate total size for better messaging
    const totalSize = bulkUploadForm.files.reduce((acc, f) => acc + f.size, 0)
    const sizeFormatted = formatBytes(totalSize)
    
    // Create abort signal object
    const abortSignal = { aborted: false }
    uploadAbortRef.current = { abort: () => { abortSignal.aborted = true } }
    
    setUploadProgress({
      currentFile: '',
      currentIndex: 0,
      totalFiles: bulkUploadForm.files.length,
      completedCount: 0,
      failedCount: 0,
      processingMessage: 'Starting batched upload...',
      bytesLoaded: 0,
      bytesTotal: totalSize,
      percentComplete: 0,
      phase: 'uploading',
    })
    setUploadStatus(`Uploading ${bulkUploadForm.files.length} files one at a time...`)
    console.log('[BulkUpload] Starting batched upload of', bulkUploadForm.files.length, 'files,', sizeFormatted)

    // Use batched upload - one file at a time
    datasetAPI.uploadBatched(
      bulkUploadForm.files,
      bulkUploadForm.state,
      {
        onFileStart: (filename, index, total) => {
          console.log(`[BulkUpload] Starting file ${index + 1}/${total}: ${filename}`)
          setUploadProgress(prev => ({
            ...prev,
            currentFile: filename,
            currentIndex: index,
            bytesLoaded: 0,
            bytesTotal: 0,
            processingMessage: `File ${index + 1}/${total}: ${filename}`,
          }))
          setUploadStatus(`Uploading file ${index + 1}/${total}: ${filename}`)
        },
        onFileProgress: (filename, loaded, total, percent) => {
          // Check for special "saving" marker (loaded === -1)
          if (loaded === -1) {
            setUploadProgress(prev => ({
              ...prev,
              bytesLoaded: prev.bytesTotal, // Show full size
              percentComplete: 100,
              processingMessage: `${filename}: Saving to server...`,
            }))
          } else {
            setUploadProgress(prev => ({
              ...prev,
              bytesLoaded: loaded,
              bytesTotal: total,
              percentComplete: percent,
              processingMessage: `${filename}: ${formatBytes(loaded)} / ${formatBytes(total)} (${percent}%)`,
            }))
          }
        },
        onFileComplete: (filename, index, success, dataset, error) => {
          console.log(`[BulkUpload] File ${index + 1} ${success ? 'succeeded' : 'failed'}: ${filename}`, error || '')
          
          setBulkUploadResults(prev => [...prev, {
            filename,
            success,
            dataset,
            error,
          }])
          
          setUploadProgress(prev => ({
            ...prev,
            completedCount: success ? prev.completedCount + 1 : prev.completedCount,
            failedCount: success ? prev.failedCount : prev.failedCount + 1,
          }))
        },
        onAllComplete: (successCount, failCount, total) => {
          console.log(`[BulkUpload] All done: ${successCount} success, ${failCount} failed`)
          uploadAbortRef.current = null
          
          setUploadProgress(prev => ({
            ...prev,
            phase: 'complete',
            processingMessage: `Completed: ${successCount}/${total} files`,
          }))
          setUploadStatus(`Completed: ${successCount}/${total} files uploaded successfully`)
          
          if (failCount === 0) {
            toast.success(`All ${total} files uploaded successfully!`)
          } else {
            toast.warning(`${successCount} files uploaded, ${failCount} failed`)
          }
          
          setUploading(false)
          loadData()
        },
        onError: (error) => {
          console.error('[BulkUpload] Error:', error)
          uploadAbortRef.current = null
          setUploadProgress(prev => ({ ...prev, phase: 'complete' }))
          setUploadStatus(`Error: ${error.message}`)
          if (error.message !== 'Upload cancelled') {
            toast.error(error.message)
          }
          setUploading(false)
        },
      },
      abortSignal
    )
  }
  
  const handleCancelUpload = () => {
    if (uploadAbortRef.current) {
      uploadAbortRef.current.abort()
      uploadAbortRef.current = null
      setUploading(false)
      setUploadProgress(prev => ({ ...prev, phase: 'complete' }))
      setUploadStatus('Upload cancelled')
      toast.info('Upload cancelled')
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

  // Multi-select handlers
  const toggleSelectDataset = (id: number) => {
    setSelectedDatasets(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const toggleSelectAll = () => {
    if (selectedDatasets.size === datasets.length) {
      setSelectedDatasets(new Set())
    } else {
      setSelectedDatasets(new Set(datasets.map(d => d.id)))
    }
  }

  const handleBulkDelete = async () => {
    if (selectedDatasets.size === 0) return
    
    if (!confirm(`Are you sure you want to delete ${selectedDatasets.size} dataset(s)?`)) {
      return
    }

    setDeletingMultiple(true)
    let successCount = 0
    let failCount = 0

    for (const id of selectedDatasets) {
      try {
        await datasetAPI.delete(id)
        successCount++
      } catch (err) {
        failCount++
        console.error(`Failed to delete dataset ${id}:`, err)
      }
    }

    setDeletingMultiple(false)
    setSelectedDatasets(new Set())
    
    if (failCount === 0) {
      toast.success(`Deleted ${successCount} dataset(s)`)
    } else {
      toast.warning(`Deleted ${successCount}, failed ${failCount}`)
    }
    
    loadData()
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
            <CardTitle>Upload Datasets</CardTitle>
            <CardDescription>
              Upload county address data in GeoJSON format (.geojson or .geojson.gz)
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="flex space-x-4">
              <Button onClick={() => setUploadModalOpen(true)}>
                <Upload className="mr-2 h-4 w-4" />
                Single Upload
              </Button>
              <Button variant="outline" onClick={() => setBulkUploadModalOpen(true)}>
                <Files className="mr-2 h-4 w-4" />
                Bulk Upload
              </Button>
            </div>
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
              <div className="flex items-center space-x-2">
                {/* Bulk delete button */}
                {selectedDatasets.size > 0 && (
                  <Button
                    variant="destructive"
                    size="sm"
                    onClick={handleBulkDelete}
                    disabled={deletingMultiple}
                  >
                    {deletingMultiple ? (
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    ) : (
                      <Trash2 className="mr-2 h-4 w-4" />
                    )}
                    Delete {selectedDatasets.size} Selected
                  </Button>
                )}
                
                <Select value={statusFilter} onValueChange={setStatusFilter}>
                  <SelectTrigger className="w-[140px]">
                    <SelectValue placeholder="All Status" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">All Status</SelectItem>
                    <SelectItem value="pending">Pending</SelectItem>
                    <SelectItem value="processing">Processing</SelectItem>
                    <SelectItem value="completed">Completed</SelectItem>
                    <SelectItem value="failed">Failed</SelectItem>
                  </SelectContent>
                </Select>
                
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
                  <TableHead className="w-12">
                    <Checkbox
                      checked={datasets.length > 0 && selectedDatasets.size === datasets.length}
                      onCheckedChange={toggleSelectAll}
                      aria-label="Select all"
                    />
                  </TableHead>
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
                    <TableCell colSpan={8} className="text-center text-muted-foreground">
                      No datasets uploaded yet
                    </TableCell>
                  </TableRow>
                ) : (
                  datasets.map((dataset) => (
                    <TableRow key={dataset.id} className={selectedDatasets.has(dataset.id) ? 'bg-muted/50' : ''}>
                      <TableCell>
                        <Checkbox
                          checked={selectedDatasets.has(dataset.id)}
                          onCheckedChange={() => toggleSelectDataset(dataset.id)}
                          aria-label={`Select ${dataset.name}`}
                        />
                      </TableCell>
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
            
            {(uploading || uploadStatus) && (
              <div className="flex items-center space-x-2 p-3 bg-muted rounded-md">
                {uploading && <Loader2 className="h-4 w-4 animate-spin" />}
                {uploadStatus.includes('complete') && <CheckCircle className="h-4 w-4 text-green-500" />}
                {uploadStatus.includes('Error') && <XCircle className="h-4 w-4 text-red-500" />}
                <p className="text-sm">{uploadStatus}</p>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => { setUploadModalOpen(false); setUploadStatus('') }} disabled={uploading}>
              Cancel
            </Button>
            <Button onClick={handleUpload} disabled={uploading}>
              {uploading ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Upload className="mr-2 h-4 w-4" />}
              Upload
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Bulk Upload Modal */}
      <Dialog open={bulkUploadModalOpen} onOpenChange={(open) => {
        setBulkUploadModalOpen(open)
        if (!open) {
          setUploadStatus('')
          setBulkUploadResults([])
        }
      }}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Bulk Upload Datasets</DialogTitle>
            <DialogDescription>
              Upload multiple county address datasets at once. County names will be extracted from filenames
              (e.g., "adams-addresses-county.geojson.gz" → Adams County)
            </DialogDescription>
          </DialogHeader>
          
          <div className="space-y-4">
            <div>
              <Label htmlFor="bulk-state">State Code (applies to all files)</Label>
              <Input
                id="bulk-state"
                placeholder="e.g., OH"
                value={bulkUploadForm.state}
                onChange={(e) => setBulkUploadForm({ ...bulkUploadForm, state: e.target.value.toUpperCase() })}
                maxLength={2}
              />
            </div>
            
            <div>
              <Label htmlFor="bulk-files">Files (.geojson or .geojson.gz)</Label>
              <Input
                id="bulk-files"
                type="file"
                accept=".geojson,.json,.gz"
                multiple
                ref={bulkFileInputRef}
                onChange={handleBulkFileChange}
              />
              {bulkUploadForm.files.length > 0 && (
                <div className="mt-2 space-y-1">
                  <p className="text-sm font-medium">{bulkUploadForm.files.length} files selected:</p>
                  <div className="max-h-32 overflow-y-auto bg-muted rounded-md p-2">
                    {bulkUploadForm.files.map((file, idx) => (
                      <div key={idx} className="text-sm text-muted-foreground flex justify-between">
                        <span>{file.name}</span>
                        <span>{formatBytes(file.size)}</span>
                      </div>
                    ))}
                  </div>
                  <p className="text-sm text-muted-foreground">
                    Total size: {formatBytes(bulkUploadForm.files.reduce((acc, f) => acc + f.size, 0))}
                  </p>
                </div>
              )}
            </div>
            
            {(uploading || uploadStatus) && (
              <div className="space-y-2 p-3 bg-muted rounded-md">
                <div className="flex items-center space-x-2">
                  {uploading && <Loader2 className="h-4 w-4 animate-spin" />}
                  {uploadStatus.includes('Completed') && <CheckCircle className="h-4 w-4 text-green-500" />}
                  {uploadStatus.includes('Error') && <XCircle className="h-4 w-4 text-red-500" />}
                  {uploadStatus.includes('cancelled') && <XCircle className="h-4 w-4 text-yellow-500" />}
                  <p className="text-sm">{uploadStatus}</p>
                </div>
                
                {/* Overall progress for batched upload */}
                {uploading && uploadProgress.phase === 'uploading' && (
                  <div className="space-y-2">
                    {/* Overall file count progress */}
                    <div className="space-y-1">
                      <div className="flex justify-between text-xs text-muted-foreground">
                        <span>Overall Progress</span>
                        <span className="flex items-center space-x-2">
                          {uploadProgress.completedCount > 0 && (
                            <span className="text-green-600">✓ {uploadProgress.completedCount}</span>
                          )}
                          {uploadProgress.failedCount > 0 && (
                            <span className="text-red-600">✗ {uploadProgress.failedCount}</span>
                          )}
                          <span>{uploadProgress.currentIndex + 1} / {uploadProgress.totalFiles} files</span>
                        </span>
                      </div>
                      <Progress 
                        value={((uploadProgress.completedCount + uploadProgress.failedCount) / uploadProgress.totalFiles) * 100} 
                      />
                    </div>
                    
                    {/* Current file progress */}
                    {uploadProgress.currentFile && (
                      <div className="space-y-1 pl-2 border-l-2 border-primary/30">
                        <div className="flex justify-between text-xs text-muted-foreground">
                          <span className="truncate max-w-[200px]">{uploadProgress.currentFile}</span>
                          <span className="font-medium">{uploadProgress.percentComplete}%</span>
                        </div>
                        <Progress value={uploadProgress.percentComplete} className="h-1" />
                        {uploadProgress.bytesTotal > 0 && (
                          <p className="text-xs text-muted-foreground">
                            {formatBytes(uploadProgress.bytesLoaded)} / {formatBytes(uploadProgress.bytesTotal)}
                          </p>
                        )}
                      </div>
                    )}
                  </div>
                )}
                
                {/* Show processing status after upload completes */}
                {uploadProgress.phase === 'processing' && (
                  <div className="space-y-1">
                    <Progress value={100} />
                    <p className="text-xs text-muted-foreground">
                      Files uploaded, processing on server...
                    </p>
                  </div>
                )}
                
                {/* Show file results after complete */}
                {uploadProgress.phase === 'complete' && uploadProgress.completedCount + uploadProgress.failedCount > 0 && (
                  <div className="flex justify-between text-xs text-muted-foreground">
                    <span>
                      {uploadProgress.completedCount + uploadProgress.failedCount} / {uploadProgress.totalFiles} files
                    </span>
                    <span className="flex items-center space-x-2">
                      {uploadProgress.completedCount > 0 && (
                        <span className="text-green-600">✓ {uploadProgress.completedCount}</span>
                      )}
                      {uploadProgress.failedCount > 0 && (
                        <span className="text-red-600">✗ {uploadProgress.failedCount}</span>
                      )}
                    </span>
                  </div>
                )}
              </div>
            )}

            {/* Upload Results */}
            {bulkUploadResults.length > 0 && (
              <div className="border rounded-md p-3">
                <p className="text-sm font-medium mb-2">Upload Results:</p>
                <div className="max-h-48 overflow-y-auto space-y-1">
                  {bulkUploadResults.map((result, idx) => (
                    <div key={idx} className="flex items-center justify-between text-sm">
                      <span className="flex items-center space-x-2">
                        {result.success ? (
                          <CheckCircle className="h-4 w-4 text-green-500" />
                        ) : (
                          <XCircle className="h-4 w-4 text-red-500" />
                        )}
                        <span>{result.filename}</span>
                      </span>
                      {result.error && (
                        <span className="text-red-500 text-xs">{result.error}</span>
                      )}
                      {result.dataset && (
                        <Badge variant="outline">{result.dataset.county}</Badge>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button 
              variant={uploading ? "destructive" : "outline"}
              onClick={() => { 
                if (uploading) {
                  handleCancelUpload()
                } else {
                  setBulkUploadModalOpen(false)
                  setUploadStatus('')
                  setBulkUploadResults([])
                  setBulkUploadForm({ state: '', files: [] })
                  setUploadProgress({
                    currentFile: '',
                    currentIndex: 0,
                    totalFiles: 0,
                    completedCount: 0,
                    failedCount: 0,
                    processingMessage: '',
                    bytesLoaded: 0,
                    bytesTotal: 0,
                    percentComplete: 0,
                    phase: 'idle',
                  })
                  if (bulkFileInputRef.current) bulkFileInputRef.current.value = ''
                }
              }} 
            >
              {uploading ? 'Cancel Upload' : bulkUploadResults.length > 0 ? 'Close' : 'Cancel'}
            </Button>
            <Button onClick={handleBulkUpload} disabled={uploading || bulkUploadResults.length > 0}>
              {uploading ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Files className="mr-2 h-4 w-4" />}
              Upload {bulkUploadForm.files.length > 0 ? `${bulkUploadForm.files.length} Files` : 'Files'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
