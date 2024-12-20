import { defineConfig } from `vite`;
import tailwindcss from '@tailwindcss/vite';
import { compression } from 'vite-plugin-compression2'

export default defineConfig({

  server: {
    proxy: {
      '/r': 'http://localhost:8080'
    }
  },
  plugins: [
    tailwindcss(),
    compression(),
    compression({ algorithm: 'brotliCompress' })
  ],
  build: {
    rollupOptions: {
      output: {
        entryFileNames: `[name].js`,
        chunkFileNames: `[name].js`,
        assetFileNames: `[name].[ext]`
      }
    }
  }
});
