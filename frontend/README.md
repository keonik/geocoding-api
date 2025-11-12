# GeoCode API Frontend

Modern React + TypeScript frontend built with Vite, shadcn/ui, and TanStack Router.

## Tech Stack

- **Framework**: React 18 with TypeScript
- **Build Tool**: Vite 6
- **Router**: TanStack Router (file-based routing)
- **UI Components**: shadcn/ui (Radix UI + Tailwind CSS)
- **Styling**: Tailwind CSS with custom design tokens
- **Icons**: Lucide React
- **Type Safety**: Full TypeScript with strict mode

## Project Structure

```
frontend/
├── src/
│   ├── api/              # API client functions
│   │   ├── auth.ts       # Authentication API
│   │   ├── apiKeys.ts    # API key management
│   │   └── usage.ts      # Usage statistics
│   ├── components/
│   │   └── ui/           # shadcn/ui components
│   ├── lib/
│   │   ├── api-client.ts # Base API client with JWT auth
│   │   └── utils.ts      # Utility functions (cn, etc.)
│   ├── routes/           # File-based routing
│   │   ├── __root.tsx    # Root layout
│   │   ├── index.tsx     # Home redirect
│   │   ├── dashboard.tsx # User dashboard
│   │   └── auth/
│   │       ├── signin.tsx
│   │       └── signup.tsx
│   ├── types/
│   │   └── api.ts        # TypeScript type definitions
│   ├── index.css         # Global styles & Tailwind
│   ├── main.tsx          # App entry point
│   └── router.tsx        # Router configuration
├── index.html
├── vite.config.ts
├── tsconfig.json
├── tailwind.config.js
└── package.json
```

## Development

### Prerequisites

- Node.js 18+ and npm
- Running Go backend on port 8080

### Setup

```bash
# Install dependencies
npm install

# Start dev server (with proxy to backend)
npm run dev
```

The dev server will run on `http://localhost:5173` with API requests proxied to the Go backend at `http://localhost:8080`.

### Build

```bash
# Build for production
npm run build
```

Output goes to `../static-new/` which the Go backend serves.

### Lint

```bash
npm run lint
```

## Features

### Authentication
- JWT-based authentication
- Automatic token storage in localStorage
- Protected routes with automatic redirects
- Login & signup with form validation

### Dashboard
- API key management (create, view, delete)
- Usage statistics display
- Real-time key copying
- Permission-based key creation

### API Client
- Centralized fetch wrapper with JWT injection
- TypeScript types for all API responses
- Automatic error handling
- Token refresh ready

### UI Components
All components are from shadcn/ui (customizable, accessible):
- Button
- Card
- Dialog
- Input
- Label
- Tabs

## Routing

Uses TanStack Router with file-based routing:

- `/` - Auto-redirects to dashboard or signin
- `/auth/signin` - Login page
- `/auth/signup` - Registration page
- `/dashboard` - User dashboard (protected)

Routes are type-safe with automatic route tree generation.

## API Integration

### Base Client

```typescript
import { fetchAPI } from '@/lib/api-client'

// Automatically adds JWT token from localStorage
const data = await fetchAPI<ResponseType>('/api/v1/endpoint')
```

### API Modules

```typescript
import { authAPI } from '@/api/auth'
import { apiKeysAPI } from '@/api/apiKeys'
import { usageAPI } from '@/api/usage'

// All typed and ready to use
const response = await authAPI.login({ email, password })
```

## Styling

Uses Tailwind CSS with custom design system:

- CSS variables for theming (supports dark mode)
- Consistent spacing, colors, and typography
- Responsive by default
- Utility-first approach

### Customization

Edit `tailwind.config.js` and `src/index.css` to customize the design system.

## TypeScript

Full type safety across:
- API request/response types
- Router routes and navigation
- Component props
- Form data

Type definitions in `src/types/api.ts`.

## Deployment

The build output is served by the Go backend:

1. Build: `npm run build` → outputs to `../static-new/`
2. Go serves at `static-new/` with SPA fallback
3. All routes handled by React Router
4. API routes proxied through `/api/v1`

## Environment Variables

Create `.env` for custom API URL:

```env
VITE_API_BASE_URL=http://localhost:8080
```

Otherwise defaults to empty string (same origin).

## Next Steps

- [ ] Add admin dashboard route
- [ ] Implement usage charts/graphs
- [ ] Add API key expiration management
- [ ] Implement dark mode toggle
- [ ] Add form validation library (zod + react-hook-form)
- [ ] Add loading skeletons
- [ ] Add toast notifications
- [ ] Add error boundaries
- [ ] Add unit tests (Vitest)
- [ ] Add E2E tests (Playwright)

## Scripts

```bash
npm run dev      # Start dev server
npm run build    # Build for production
npm run preview  # Preview production build locally
npm run lint     # Run ESLint
```

## License

Same as main project
