/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  // Relative asset URLs so the bundle works under wails://, file-less
  // embedding, and the browser `serve` mode alike.
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022',
  },
  server: {
    port: 5174,
    strictPort: true,
    // `npm run dev` against a running `sheetdiver serve` (default port).
    proxy: {
      '/api': 'http://127.0.0.1:47480',
      '/mcp': 'http://127.0.0.1:47480',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['./src/test-setup.ts'],
    css: false,
  },
});
