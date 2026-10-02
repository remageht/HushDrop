import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'https://127.0.0.1:8443',
        secure: false, // self-signed cert in dev
        changeOrigin: true
      },
      '/health': {
        target: 'https://127.0.0.1:8443',
        secure: false,
        changeOrigin: true
      }
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  }
});
