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
      // paths like /api-keys (an SPA route) and /api-docs.yaml.
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
