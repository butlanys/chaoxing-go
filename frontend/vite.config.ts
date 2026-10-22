import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Wails 会把 frontend/dist 通过 //go:embed all:frontend/dist 打进二进制,
// 因此 outDir 必须是 dist 且在每次构建前清空。
export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2020',
    sourcemap: false,
    chunkSizeWarningLimit: 2000,
  },
  server: {
    port: 34115,
    strictPort: false,
  },
})
