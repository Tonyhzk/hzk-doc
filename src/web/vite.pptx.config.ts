import { defineConfig } from 'vite'

export default defineConfig({
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },
  build: {
    lib: {
      entry: 'src/pptx-frame.ts',
      formats: ['iife'],
      name: 'PptxFrame',
      fileName: () => 'pptx-frame.js',
    },
    outDir: 'build',
    emptyOutDir: true,
  },
})
