import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const apiProxy = {
  '/api': {
    target: 'http://localhost:8080',
    changeOrigin: true,
  },
}

const upstreamAPIBaseURL = 'http://localhost:8080'

const fallbackSystemConfig = {
  initialized: true,
  data_gateway_url: 'http://127.0.0.1:8090',
}

function systemConfigFallbackPlugin() {
  return {
    name: 'nofx-system-config-fallback',
    configureServer(server) {
      server.middlewares.use(handleSystemConfigFallback)
    },
    configurePreviewServer(server) {
      server.middlewares.use(handleSystemConfigFallback)
    },
  }
}

async function handleSystemConfigFallback(req, res, next) {
  const path = req.url?.split('?')[0]
  if (path === '/api/config' && req.method === 'GET') {
    await forwardOrFallback(req, res, fallbackSystemConfig, 200, 500)
    return
  }

  next()
}

async function forwardOrFallback(req, res, fallbackBody, fallbackStatus, timeoutMS) {
  try {
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), timeoutMS)
    const upstream = await fetch(`${upstreamAPIBaseURL}${req.url}`, {
      signal: controller.signal,
    })
    clearTimeout(timeout)

    res.statusCode = upstream.status
    res.setHeader('Content-Type', upstream.headers.get('Content-Type') || 'application/json')
    res.end(await upstream.text())
    return
  } catch {
    writeJSON(res, fallbackStatus, fallbackBody)
  }
}

function writeJSON(res, status, body) {
  res.statusCode = status
  res.setHeader('Content-Type', 'application/json')
  res.end(JSON.stringify(body))
}

export default defineConfig({
  plugins: [react(), systemConfigFallbackPlugin()],
  server: {
    host: '0.0.0.0',
    port: 3000,
    proxy: apiProxy,
  },
  preview: {
    proxy: apiProxy,
  },
})
