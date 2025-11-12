# Frontend Migration Guide

## What Changed

The GeoCode API now has a modern React frontend built with Vite, replacing the old static HTML files.

### Before (Old)
- Static HTML files (`static/signin.html`, `dashboard.html`, etc.)
- Vanilla JavaScript
- Manual DOM manipulation
- No build process
- Global variables and script tags

### After (New)
- React 18 + TypeScript
- Vite build system
- shadcn/ui components
- TanStack Router (type-safe routing)
- Modular code organization

## Directory Structure

```
geocoding-api/
├── frontend/              # New React app (source)
│   ├── src/
│   │   ├── api/          # API client modules
│   │   ├── components/   # React components
│   │   ├── routes/       # File-based routes
│   │   ├── types/        # TypeScript types
│   │   └── lib/          # Utilities
│   ├── package.json
│   └── vite.config.ts
├── static/               # Old HTML files (fallback)
└── static-new/           # Built React app (production)
```

## Development Workflow

### Working on Frontend

```bash
cd frontend

# Install dependencies (first time)
npm install

# Start dev server with HMR
npm run dev
# Opens http://localhost:5173
# API calls proxy to http://localhost:8080
```

### Building for Production

```bash
cd frontend
npm run build
# Outputs to ../static-new/
```

### Running Full Stack

Terminal 1 - Backend:
```bash
cd geocoding-api
go run main.go
# Serves on http://localhost:8080
```

Terminal 2 - Frontend (dev mode):
```bash
cd frontend
npm run dev
# Dev server on http://localhost:5173
# Proxies API calls to :8080
```

OR use production build:
```bash
cd frontend
npm run build
# Then access http://localhost:8080
# Go server serves built React app
```

## Key Features

### Type Safety
- Full TypeScript coverage
- API response types
- Type-safe routing
- No `any` types

### Modern React Patterns
```typescript
// Hooks for state management
const [apiKeys, setApiKeys] = useState<APIKey[]>([])

// Type-safe navigation
navigate({ to: '/dashboard' })

// Async/await with error handling
try {
  const response = await apiKeysAPI.list()
} catch (err) {
  // Handle error
}
```

### Component Architecture
```typescript
// Reusable UI components
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

// Modular API clients
import { authAPI } from '@/api/auth'
```

### Authentication Flow
1. User signs in → JWT token returned
2. Token stored in `localStorage`
3. All API calls include `Authorization: Bearer <token>`
4. Protected routes redirect if no token

## Migrating Pages

If you need to add new pages:

### 1. Create Route File

```typescript
// src/routes/my-page.tsx
import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/my-page')({
  component: MyPage,
})

function MyPage() {
  return <div>My Page Content</div>
}
```

### 2. Build
```bash
npm run build
```

The route is automatically added to the router!

## Environment Variables

### Frontend (.env in frontend/)
```env
VITE_API_BASE_URL=http://localhost:8080
```

### Backend (.env in root)
```env
PORT=8080
JWT_SECRET=your-secret-here
DATABASE_URL=postgres://...
```

## Deployment

### Production Build
```bash
cd frontend
npm run build
```

Outputs to `static-new/` which Go serves:
- Main app: `/`
- Assets: `/assets/*`
- SPA routing handled by React Router

### Backend Configuration
The Go server checks for `static-new/` first, falls back to `static/`:

```go
staticDir := "static-new"
if _, err := os.Stat(staticDir); os.IsNotExist(err) {
    staticDir = "static"  // Fallback
}
```

## Troubleshooting

### Frontend won't build
```bash
cd frontend
rm -rf node_modules package-lock.json
npm install
npm run build
```

### API calls failing in dev mode
Check `vite.config.ts` proxy settings:
```typescript
server: {
  proxy: {
    '/api': {
      target: 'http://localhost:8080',
      changeOrigin: true,
    },
  },
}
```

### Routes not working
Rebuild to regenerate route tree:
```bash
npm run build
```

### TypeScript errors
Check `tsconfig.json` and ensure all types are imported correctly.

## Adding Features

### New API Endpoint

1. Add type in `src/types/api.ts`:
```typescript
export interface MyData {
  id: string
  name: string
}
```

2. Create API client in `src/api/mydata.ts`:
```typescript
import { fetchAPI } from '@/lib/api-client'
import type { APIResponse, MyData } from '@/types/api'

export const myDataAPI = {
  list: async (): Promise<APIResponse<MyData[]>> => {
    return fetchAPI('/api/v1/mydata')
  },
}
```

3. Use in component:
```typescript
import { myDataAPI } from '@/api/mydata'

const data = await myDataAPI.list()
```

### New UI Component

shadcn/ui makes it easy:
```bash
# Add new component (they're in src/components/ui/)
# Edit tailwind.config.js if needed
```

## Testing

```bash
# Lint
npm run lint

# Type check
npm run build

# Run dev server
npm run dev
```

## Next Steps

- Add admin dashboard route
- Implement charts for usage stats
- Add form validation (zod)
- Add loading states
- Add error boundaries
- Add unit tests
- Add E2E tests

## Resources

- [Vite Docs](https://vitejs.dev/)
- [React Docs](https://react.dev/)
- [TanStack Router](https://tanstack.com/router)
- [shadcn/ui](https://ui.shadcn.com/)
- [Tailwind CSS](https://tailwindcss.com/)
