import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tanstackRouter({
      routesDirectory: './src/routes',
      generatedRouteTree: './src/routeTree.gen.ts',
    }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    // NB: do not proxy '/auth' here. The backend mounts auth under
    // /api/v1/auth (main.go: api.Group("/auth")), so a '/auth' rule serves
    // no backend route and shadows the SPA's own /auth/signin and
    // /auth/signup routes, 404ing them on direct load or refresh.
    proxy: {
      // Regex, not the bare '/api' prefix: a prefix key also swallows sibling
      // paths such as /api-docs.yaml, which has its own rule below. Note the
      // Go server has the same hazard for a different reason -- Echo's router
      // builds a static '/api' node from the /api/v1 group, so any unmatched
      // /api* path 404s instead of reaching the SPA fallback. Do not name a
      // frontend route with an /api prefix.
      '^/api/': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/docs': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/api-docs.yaml': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../static-new',
    emptyOutDir: true,
  },
})
