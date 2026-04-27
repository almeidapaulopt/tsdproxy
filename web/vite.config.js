import { defineConfig } from 'vite';
import { cpSync, mkdirSync, existsSync } from 'fs';
import { resolve } from 'path';
import tailwindcss from '@tailwindcss/vite';
import { compression } from 'vite-plugin-compression2';
import { VitePWA } from 'vite-plugin-pwa'


function copyIconsToPublic() {
  let isBuild = false;

  return {
    name: 'copy-icons-to-public',

    config(config, { command }) {
      isBuild = command === 'build';
    },

    buildStart() {
      if (!isBuild) return;

      const targets = [
        {
          src: resolve(__dirname, 'node_modules/simple-icons/icons'),
          dest: resolve(__dirname, 'public/icons/si'),
        },
        {
          src: resolve(__dirname, 'node_modules/@mdi/svg/svg'),
          dest: resolve(__dirname, 'public/icons/mdi'),
        },
      ];

      for (const { src, dest } of targets) {
        if (!existsSync(dest)) {
          mkdirSync(dest, { recursive: true });
        }
        cpSync(src, dest, { recursive: true });
      }
    }
  };
}


export default defineConfig({

  server: {
    proxy: {
      '/list': 'http://localhost:8080',
      '/stream': 'http://localhost:8080'
    }
  },
  plugins: [
    copyIconsToPublic(),

    compression({
      filter: /\.(js|css|html|svg|ico|json|txt|woff2?|ttf)$/,
    }),
    compression({
      algorithm: 'brotliCompress',
      filter: /\.(js|css|html|svg|ico|json|txt|woff2?|ttf)$/,
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
