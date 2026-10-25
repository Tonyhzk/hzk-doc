import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig(({ command }) => {
  const backend = process.env.HZK_DOC_DEV_BACKEND?.trim()
  if (command === 'serve' && !backend) {
    throw new Error(
      '缺少 HZK_DOC_DEV_BACKEND 环境变量，请设置为正在运行的 Go 后端地址（含协议和端口）。',
    )
  }

  return {
    plugins: [vue()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server:
      command === 'serve' && backend
        ? {
            host: '0.0.0.0',
            proxy: {
              '/api': backend,
              '/file': backend,
              '/view': backend,
            },
          }
        : undefined,
    build: {
      outDir: '../internal/webui/dist',
      emptyOutDir: true,
      assetsDir: 'assets',
      chunkSizeWarningLimit: 1500,
    },
  }
})
