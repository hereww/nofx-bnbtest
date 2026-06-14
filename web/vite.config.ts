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

  if ((path === '/api/onchain/token-analysis' || path === '/api/onchain/index-status' || path === '/api/onchain/wallet-graph' || path === '/api/onchain/early-wallet-flow') && req.method === 'GET') {
    const params = new URLSearchParams(req.url?.split('?')[1] || '')
    if (path === '/api/onchain/wallet-graph') {
      await forwardOrFallback(req, res, {
        success: false,
        chain: params.get('chain') || 'bsc',
        address: params.get('address') || '',
        depth: params.get('depth') || 'recent',
        status: 'backend_unavailable',
        error: '链上分析后端未启动。请启动 NOFX API 服务后再查看钱包关系网。',
        nodes: [],
        edges: [],
      }, 503, 30_000)
      return
    }
    if (path === '/api/onchain/early-wallet-flow') {
      await forwardOrFallback(req, res, {
        success: false,
        chain: params.get('chain') || 'bsc',
        address: params.get('address') || '',
        status: 'backend_unavailable',
        completeness: 'unavailable',
        seed_count: 100,
        max_depth: 4,
        summary: {
          direction: 'insufficient_data',
          confidence: 'low',
          seed_wallet_count: 0,
          tracked_wallet_count: 0,
          max_observed_depth: 0,
          total_initial_buy_amount: 0,
          total_initial_cost: 0,
          seed_own_sell_amount: 0,
          seed_own_sell_value: 0,
          descendant_sell_amount: 0,
          descendant_sell_value: 0,
          total_sell_value: 0,
          realized_pnl: 0,
          realized_pnl_pct: 0,
          remaining_amount: 0,
          remaining_cost: 0,
          transfer_out_amount: 0,
          cost_coverage_pct: 0,
          missing_swap_price_count: 0,
          incomplete_reasons: ['backend_unavailable'],
        },
        error: '链上分析后端未启动。请启动 NOFX API 服务后再计算早期地址资金流。',
      }, 503, 30_000)
      return
    }
    await forwardOrFallback(req, res, {
      success: false,
      chain: params.get('chain') || 'bsc',
      address: params.get('address') || '',
      depth: params.get('depth') || 'recent',
      status: 'backend_unavailable',
      completeness: 'unavailable',
      error: '链上分析后端未启动。请启动 NOFX API 服务后再分析；当前预览只显示管理器和监控配置。',
    }, 503, path === '/api/onchain/token-analysis' ? 30_000 : 5_000)
    return
  }

  if ((path === '/api/onchain/index-token' || path === '/api/onchain/early-wallet-flow/index') && req.method === 'POST') {
    if (!(await isUpstreamAvailable())) {
      writeJSON(res, 503, {
        success: false,
        status: 'backend_unavailable',
        error: '链上索引后端未启动。请启动 NOFX API 服务后再启动索引。',
      })
      return
    }
    next()
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

async function isUpstreamAvailable() {
  try {
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), 500)
    const upstream = await fetch(`${upstreamAPIBaseURL}/api/config`, {
      signal: controller.signal,
    })
    clearTimeout(timeout)
    return upstream.ok
  } catch {
    return false
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
