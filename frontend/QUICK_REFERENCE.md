# Quick Reference

## Commands

```bash
# Frontend Development
cd frontend
npm install          # Install dependencies
npm run dev          # Dev server (http://localhost:5173)
npm run build        # Build for production
npm run lint         # Run linter

# Backend
go run main.go       # Start server (http://localhost:8080)
go build -o server   # Build binary

# Full Stack
Terminal 1: go run main.go
Terminal 2: cd frontend && npm run dev
```

## File Locations

```
frontend/src/
├── api/              # API calls (auth, apiKeys, usage)
├── components/ui/    # UI components (button, card, dialog, etc.)
├── routes/           # Pages (signin, signup, dashboard)
├── types/api.ts      # TypeScript types
├── lib/api-client.ts # Base API client
└── router.tsx        # Router config
```

## Common Tasks

### Add New Route
```typescript
// frontend/src/routes/my-page.tsx
import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/my-page')({
  component: MyPage,
})

function MyPage() {
  return <div>Content</div>
}
```

### Add New API Endpoint
```typescript
// frontend/src/api/mydata.ts
import { fetchAPI } from '@/lib/api-client'

export const myDataAPI = {
  list: async () => {
    return fetchAPI('/api/v1/mydata')
  },
}
```

### Use API in Component
```typescript
import { apiKeysAPI } from '@/api/apiKeys'
import { useState, useEffect } from 'react'

function MyComponent() {
  const [data, setData] = useState([])
  
  useEffect(() => {
    const load = async () => {
      const response = await apiKeysAPI.list()
      if (response.success) {
        setData(response.data.api_keys)
      }
    }
    load()
  }, [])
  
  return <div>{/* render data */}</div>
}
```

### Add UI Component
```typescript
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'

<Card>
  <CardHeader>
    <CardTitle>Title</CardTitle>
  </CardHeader>
  <CardContent>
    <Button onClick={() => {}}>Click Me</Button>
  </CardContent>
</Card>
```

## Styling

### Tailwind Classes
```typescript
<div className="flex items-center justify-between p-4 bg-white rounded-lg shadow">
  <h1 className="text-2xl font-bold">Title</h1>
  <Button className="ml-4">Action</Button>
</div>
```

### Responsive
```typescript
<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
  {/* Mobile: 1 col, Tablet: 2 cols, Desktop: 3 cols */}
</div>
```

### Custom Styles
```typescript
// Use cn() utility for conditional classes
import { cn } from '@/lib/utils'

<div className={cn(
  "base-classes",
  isActive && "active-classes",
  isPrimary ? "primary" : "secondary"
)}>
```

## Navigation

```typescript
import { useNavigate } from '@tanstack/react-router'

function MyComponent() {
  const navigate = useNavigate()
  
  const handleClick = () => {
    navigate({ to: '/dashboard' })
  }
}
```

## Authentication

### Check Auth Status
```typescript
const token = localStorage.getItem('authToken')
const user = JSON.parse(localStorage.getItem('user') || '{}')
```

### Protected Route
```typescript
import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/protected')({
  beforeLoad: () => {
    const token = localStorage.getItem('authToken')
    if (!token) {
      throw redirect({ to: '/auth/signin' })
    }
  },
  component: ProtectedPage,
})
```

### Logout
```typescript
import { authAPI } from '@/api/auth'

const handleLogout = () => {
  authAPI.logout()
  window.location.href = '/'
}
```

## TypeScript Types

### API Response
```typescript
import type { APIResponse, User, APIKey } from '@/types/api'

const response: APIResponse<User> = await authAPI.getProfile()
if (response.success && response.data) {
  const user: User = response.data
}
```

### Component Props
```typescript
interface MyComponentProps {
  title: string
  count?: number
  onSave: () => void
}

function MyComponent({ title, count = 0, onSave }: MyComponentProps) {
  return <div>{title} - {count}</div>
}
```

## State Management

```typescript
import { useState } from 'react'

// Simple state
const [count, setCount] = useState(0)
const [user, setUser] = useState<User | null>(null)
const [loading, setLoading] = useState(false)

// Object state
const [formData, setFormData] = useState({
  email: '',
  password: '',
})

const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
  setFormData(prev => ({
    ...prev,
    [e.target.name]: e.target.value
  }))
}
```

## Error Handling

```typescript
import { APIError } from '@/lib/api-client'

try {
  const response = await apiKeysAPI.create({ name, permissions })
  if (response.success) {
    // Success
  }
} catch (err) {
  if (err instanceof APIError) {
    console.error('API Error:', err.status, err.message)
  } else {
    console.error('Unknown error:', err)
  }
}
```

## Environment Variables

```bash
# frontend/.env
VITE_API_BASE_URL=http://localhost:8080
```

```typescript
// Access in code
const apiUrl = import.meta.env.VITE_API_BASE_URL
```

## Build Output

```bash
npm run build
# Outputs to ../static-new/
# Go server automatically serves it
```

## Troubleshooting

### Build errors
```bash
cd frontend
rm -rf node_modules package-lock.json
npm install
npm run build
```

### TypeScript errors
```bash
npm run build  # Shows all TS errors
```

### Dev server won't start
```bash
# Check port 5173 is free
lsof -ti:5173 | xargs kill -9
npm run dev
```

### API calls failing
- Check backend is running on port 8080
- Check proxy config in `vite.config.ts`
- Check browser console for errors
- Check network tab in DevTools

## Resources

- Vite: https://vitejs.dev/
- React: https://react.dev/
- TanStack Router: https://tanstack.com/router
- shadcn/ui: https://ui.shadcn.com/
- Tailwind: https://tailwindcss.com/
- TypeScript: https://www.typescriptlang.org/
