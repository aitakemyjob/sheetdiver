/// <reference types="vitest/config" />
import { cpSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';

/**
 * Copies the offline translation engine (Mozilla's Bergamot, MPL-2.0) from
 * node_modules into public/bergamot, so it's served as-is: its worker
 * loads its WebAssembly and glue script by relative URL.
 */
function bergamot(): Plugin {
  const copy = () => {
    const root = dirname(fileURLToPath(import.meta.url));
    const src = join(root, 'node_modules/@browsermt/bergamot-translator');
    const dst = join(root, 'public/bergamot');
    mkdirSync(join(dst, 'worker'), { recursive: true });
    for (const f of ['translator.js', 'worker/translator-worker.js', 'worker/bergamot-translator-worker.js', 'worker/bergamot-translator-worker.wasm']) cpSync(join(src, f), join(dst, f));
  };
  return { name: 'bergamot', buildStart: copy, configureServer: copy };
}

export default defineConfig({
  plugins: [react(), bergamot()],
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
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/test-setup.ts'],
      reporter: ['text-summary', 'text'],
      // CI fails if coverage drops below these (it was 63/55/53/65%).
      thresholds: { statements: 60, branches: 50, functions: 50, lines: 60 },
    },
  },
});
