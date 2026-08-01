import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'
import { pwaManifestIcons } from './src/pwaManifestIcons.ts'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: 'autoUpdate',
      manifest: {
        name: 'Splitkauf',
        short_name: 'Splitkauf',
        display: 'standalone',
        start_url: '/',
        background_color: '#ffffff',
        theme_color: '#2e7d43',
        icons: pwaManifestIcons,
      },
    }),
  ],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  build: {
    outDir: '../ports/web/dist',
    emptyOutDir: true,
  },
})
