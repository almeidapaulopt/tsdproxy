import { defineConfig } from 'vite';
import tailwindcss from '@tailwindcss/vite';
import { compression } from 'vite-plugin-compression2';
import { VitePWA } from 'vite-plugin-pwa'
import http from 'node:http';


export default defineConfig({

  server: {
    proxy: {
      '/dashboard': 'http://localhost:8080',
      '/stream': 'http://localhost:8080',
      '/api': 'http://localhost:8080'
    }
  },
  plugins: [
    {
      name: 'proxy-root',
      configureServer(server) {
        server.middlewares.use('/', (req, res, next) => {
          if (req.url === '/' || req.url === '') {
            const opts = { hostname: 'localhost', port: 8080, path: '/', method: req.method, headers: req.headers };
            const proxy = http.request(opts, (pRes) => {
              res.writeHead(pRes.statusCode, pRes.headers);
              pRes.pipe(res, { end: true });
            });
            proxy.on('error', () => { res.writeHead(502); res.end('Backend not available'); });
            req.pipe(proxy, { end: true });
            return;
          }
          next();
        });
      }
    },
    compression({
      algorithms: ['gzip', 'brotliCompress'],
      include: /\.(js|css|html|ico|json|txt|woff2?|ttf)$/,
      exclude: /icons\//,
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
      input: {
        app: 'app.js',
        styles: 'styles.css',
      },
      output: {
        entryFileNames: (chunkInfo) => {
          if (chunkInfo.name === 'app') return 'app.js';
          return `[name]-[hash].js`;
        },
        chunkFileNames: `[name]-[hash].js`,
        assetFileNames: (assetInfo) => {
          if (assetInfo.name === 'styles.css') return 'styles.css';
          return `[name]-[hash].[ext]`;
        }
      }
    }
  }
});
