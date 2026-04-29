import { defineConfig } from 'vite';
import tailwindcss from '@tailwindcss/vite';
import { compression } from 'vite-plugin-compression2';
import { VitePWA } from 'vite-plugin-pwa'


export default defineConfig({

  server: {
    proxy: {
      '/list': 'http://localhost:8080',
      '/stream': 'http://localhost:8080'
    }
  },
  plugins: [
    compression({
      algorithms: ['gzip', 'brotliCompress'],
      include: /\.(js|css|html|svg|ico|json|txt|woff2?|ttf)$/,
    }),

    tailwindcss(),

    VitePWA({
      registerType: 'autoUpdate',
      injectRegister: false,

      pwaAssets: {
        disabled: false,
        config: true,
      },

      manifest: {
        name: 'TSDProxy',
        short_name: 'TSDProxy',
        description: 'TSDProxy',
        theme_color: '#ffffff',
      },

      workbox: {
        globPatterns: ['**/*.{js,css,html,ico}'],
        cleanupOutdatedCaches: true,
        clientsClaim: true,
      },

      devOptions: {
        enabled: false,
        navigateFallback: 'index.html',
        suppressWarnings: true,
        type: 'module',
      },
    }),
  ],

  build: {
    rollupOptions: {
      output: {
        entryFileNames: `[name]-[hash].js`,
        chunkFileNames: `[name]-[hash].js`,
        assetFileNames: `[name]-[hash].[ext]`
      }
    }
  }
});
