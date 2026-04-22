/**
 * Vite config for building the sandbox runtime bundle.
 *
 * This produces a single IIFE JS file (sandbox-runtime.js) that exposes
 * React, ReactDOM, Sucrase, and Recharts as window globals. It's loaded
 * by the generative UI iframe from the same origin.
 *
 * Build: npx vite build --config vite.config.sandbox.js
 */
import { defineConfig } from 'vite'
import { readFileSync } from 'fs'
import { resolve } from 'path'

const pkg = JSON.parse(readFileSync(resolve(__dirname, 'package.json'), 'utf-8'))
const version = pkg.version || '0.0.0'

export default defineConfig({
  define: {
    // Replace process.env.NODE_ENV at build time — the sandbox runtime runs
    // in a browser where `process` is not defined. Without this, the IIFE
    // crashes on `process is not defined` before setting window globals.
    'process.env.NODE_ENV': JSON.stringify('production'),
    'process.env': JSON.stringify({}),
  },
  build: {
    outDir: `dist/${version}`,
    // Don't empty — the main build outputs here too
    emptyOutDir: false,
    lib: {
      entry: resolve(__dirname, 'src/sandbox-runtime.ts'),
      name: 'SandboxRuntime',
      formats: ['iife'],
      fileName: () => 'sandbox-runtime.js',
    },
    rollupOptions: {
      output: {
        // Ensure everything ends up in a single file
        manualChunks: undefined,
      },
    },
    // Sandbox runtime is large (~500KB) due to React+Recharts — this is expected
    chunkSizeWarningLimit: 2000,
  },
})
