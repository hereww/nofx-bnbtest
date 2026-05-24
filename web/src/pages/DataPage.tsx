import type { ReactNode } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { Activity, BarChart3, Database, RefreshCcw, ShieldCheck, TrendingUp } from 'lucide-react'
import { useLanguage } from '../contexts/LanguageContext'
import { t } from '../i18n/translations'

type GatewayHealth = {
  success: boolean
  status: string
  source: string
  real_data: boolean
  last_updated: string
  providers?: Record<string, { status: string; message?: string; updated_at?: string }>
  counts?: { coins?: number }
}

type AI500Response = {
  success: boolean
  data?: {
    coins?: Array<{ pair: string; score: number; increase_percent: number }>
    count?: number
  }
}

type OIResponse = {
  success: boolean
  data?: {
    positions?: Array<{ symbol: string; oi_delta_percent: number; price_delta_percent: number }>
  }
}

type NetFlowResponse = {
  success: boolean
  data?: {
    netflows?: Array<{ symbol: string; amount: number; price: number }>
  }
}

type CustomToken = {
  address: string
  chain_id?: string
  dex_id?: string
  name?: string
  symbol?: string
  quote_symbol?: string
  price_usd?: string
  liquidity_usd?: number
  volume_24h_usd?: number
  fdv?: number
  market_cap?: number
  pair_url?: string
  trade_supported: boolean
  reason?: string
  error?: string
}

type CustomTokensResponse = {
  success: boolean
  source: string
  count: number
  tokens: CustomToken[]
}

export function DataPage() {
  const { language } = useLanguage()
  const [gatewayUrl, setGatewayUrl] = useState('')
  const [health, setHealth] = useState<GatewayHealth | null>(null)
  const [ai500, setAI500] = useState<AI500Response['data'] | null>(null)
  const [oi, setOI] = useState<OIResponse['data'] | null>(null)
  const [netflow, setNetflow] = useState<NetFlowResponse['data'] | null>(null)
  const [customTokens, setCustomTokens] = useState<CustomToken[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const displayUrl = useMemo(() => gatewayUrl || 'http://127.0.0.1:8090', [gatewayUrl])

  useEffect(() => {
    let cancelled = false
    async function loadGateway() {
      setLoading(true)
      setError('')
      try {
        const base = '/api/data-gateway'
        if (!cancelled) setGatewayUrl(base)

        const [healthRes, ai500Res, oiRes, netflowRes, customTokensRes] = await Promise.all([
          fetch(`${base}/health`),
          fetch(`${base}/api/ai500/list?limit=6`),
          fetch(`${base}/api/oi/top-ranking?duration=1h&limit=5`),
          fetch(`${base}/api/netflow/top-ranking?duration=1h&limit=5&type=institution&trade=future`),
          fetch('/api/custom-tokens'),
        ])
        if (!healthRes.ok) throw new Error(`Gateway health HTTP ${healthRes.status}`)
        const healthJson = (await healthRes.json()) as GatewayHealth
        const ai500Json = (await ai500Res.json()) as AI500Response
        const oiJson = (await oiRes.json()) as OIResponse
        const netflowJson = (await netflowRes.json()) as NetFlowResponse
        const customTokensJson = (await customTokensRes.json()) as CustomTokensResponse
        if (!cancelled) {
          setHealth(healthJson)
          setAI500(ai500Json.data || null)
          setOI(oiJson.data || null)
          setNetflow(netflowJson.data || null)
          setCustomTokens(customTokensJson.tokens || [])
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Gateway unavailable')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    loadGateway()
    const timer = window.setInterval(loadGateway, 30000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  return (
    <div className="min-h-[calc(100vh-64px)] bg-[#05070d] text-white">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-8 px-4 py-8 sm:px-6 lg:px-8">
        <section className="rounded-lg border border-white/10 bg-white/[0.03] p-6 shadow-2xl shadow-black/30 sm:p-8">
          <div className="flex flex-col gap-6 lg:flex-row lg:items-center lg:justify-between">
            <div className="max-w-3xl">
              <div className="mb-4 inline-flex items-center gap-2 rounded-full border border-[#F0B90B]/30 bg-[#F0B90B]/10 px-3 py-1 text-xs font-semibold uppercase tracking-wide text-[#F0B90B]">
                <Database className="h-3.5 w-3.5" />
                NOFX Data Gateway
              </div>
              <h1 className="text-3xl font-semibold tracking-normal text-white sm:text-4xl">
                {t('dataCenter', language)}
              </h1>
              <p className="mt-4 max-w-2xl text-sm leading-6 text-zinc-300 sm:text-base">
                {language === 'zh'
                  ? '本页使用独立自研数据网关。v1 通过交易所公开数据采集价格、成交额、资金费率和 OI，并在本地计算策略兼容指标。'
                  : 'This page uses the independent self-hosted data gateway. v1 collects public exchange price, volume, funding, and OI data, then calculates strategy-compatible indicators locally.'}
              </p>
            </div>

            <div className="rounded-lg border border-white/10 bg-black/25 px-4 py-3 text-sm text-zinc-300">
              <div className="text-xs uppercase text-zinc-500">Gateway URL</div>
              <div className="mt-1 font-mono text-[#F0B90B]">{displayUrl}</div>
            </div>
          </div>
        </section>

        <section className="grid gap-4 md:grid-cols-4">
          <StatusCard label="Gateway" value={loading ? 'checking' : health?.status || 'error'} tone={health?.status === 'ok' ? 'good' : 'warn'} />
          <StatusCard label="Exchange Public" value={health?.providers?.exchange_public?.status || 'unknown'} tone={health?.providers?.exchange_public?.status === 'ok' ? 'good' : 'warn'} />
          <StatusCard label="Binance Public" value={health?.providers?.binance?.status || 'unknown'} tone={health?.providers?.binance?.status === 'ok' ? 'good' : 'warn'} />
          <StatusCard label="Coins" value={String(health?.counts?.coins || ai500?.count || 0)} tone="neutral" />
        </section>

        {error && (
          <section className="rounded-lg border border-red-400/25 bg-red-400/[0.06] p-5 text-sm text-red-100">
            {language === 'zh' ? '数据网关暂时不可用：' : 'Data gateway unavailable: '}
            <span className="font-mono">{error}</span>
          </section>
        )}

        <section className="grid gap-4 lg:grid-cols-3">
          <DataList
            icon={<BarChart3 className="h-5 w-5" />}
            title="AI500"
            items={(ai500?.coins || []).map((coin) => ({
              name: coin.pair,
              value: `${coin.score.toFixed(1)} / ${coin.increase_percent.toFixed(2)}%`,
            }))}
          />
          <DataList
            icon={<TrendingUp className="h-5 w-5" />}
            title="OI Top"
            items={(oi?.positions || []).map((pos) => ({
              name: pos.symbol,
              value: `${pos.oi_delta_percent.toFixed(2)}% OI / ${pos.price_delta_percent.toFixed(2)}%`,
            }))}
          />
          <DataList
            icon={<Activity className="h-5 w-5" />}
            title="NetFlow"
            items={(netflow?.netflows || []).map((flow) => ({
              name: flow.symbol,
              value: formatUSDT(flow.amount),
            }))}
          />
        </section>

        <section className="rounded-lg border border-white/10 bg-white/[0.035] p-5">
          <div className="flex flex-col gap-3 border-b border-white/10 pb-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="text-base font-semibold text-white">
                {language === 'zh' ? '自定义代币监控' : 'Custom Token Monitor'}
              </h2>
              <p className="mt-1 text-sm text-zinc-400">
                {language === 'zh'
                  ? '按合约地址读取 DEX 行情，用于观察价格、流动性和成交额；这些不是当前 Binance Futures 可直接下单的 symbol。'
                  : 'Reads DEX market data by contract address for price, liquidity, and volume monitoring. These are not directly tradable Binance Futures symbols.'}
              </p>
            </div>
            <div className="rounded-md border border-amber-300/25 bg-amber-300/10 px-3 py-2 text-xs text-amber-100">
              {language === 'zh' ? '监控展示，不自动交易' : 'Monitor only, no auto trading'}
            </div>
          </div>

          <div className="mt-4 overflow-x-auto">
            <table className="w-full min-w-[860px] text-left text-sm">
              <thead className="text-xs uppercase text-zinc-500">
                <tr className="border-b border-white/10">
                  <th className="py-3 pr-4 font-medium">{language === 'zh' ? '代币' : 'Token'}</th>
                  <th className="py-3 pr-4 font-medium">{language === 'zh' ? '链/DEX' : 'Chain / DEX'}</th>
                  <th className="py-3 pr-4 font-medium">{language === 'zh' ? '价格' : 'Price'}</th>
                  <th className="py-3 pr-4 font-medium">{language === 'zh' ? '流动性' : 'Liquidity'}</th>
                  <th className="py-3 pr-4 font-medium">24h Vol</th>
                  <th className="py-3 pr-4 font-medium">{language === 'zh' ? '状态' : 'Status'}</th>
                </tr>
              </thead>
              <tbody>
                {customTokens.map((token) => (
                  <tr key={token.address} className="border-b border-white/[0.06]">
                    <td className="py-3 pr-4">
                      <div className="font-medium text-zinc-100">{token.symbol || '-'}</div>
                      <div className="mt-0.5 max-w-[280px] truncate font-mono text-xs text-zinc-500" title={token.address}>
                        {token.address}
                      </div>
                    </td>
                    <td className="py-3 pr-4 text-zinc-300">
                      <div>{token.chain_id || '-'}</div>
                      <div className="text-xs text-zinc-500">{token.dex_id || '-'}</div>
                    </td>
                    <td className="py-3 pr-4 font-mono text-zinc-100">
                      {token.price_usd ? `$${token.price_usd}` : '-'}
                    </td>
                    <td className="py-3 pr-4 text-zinc-300">{formatUSDT(token.liquidity_usd || 0)}</td>
                    <td className="py-3 pr-4 text-zinc-300">{formatUSDT(token.volume_24h_usd || 0)}</td>
                    <td className="py-3 pr-4">
                      {token.error ? (
                        <span className="rounded bg-red-400/10 px-2 py-1 text-xs text-red-200">{token.error}</span>
                      ) : (
                        <span className="rounded bg-cyan-400/10 px-2 py-1 text-xs text-cyan-100">
                          {language === 'zh' ? '监控中' : 'Monitoring'}
                        </span>
                      )}
                      {token.pair_url && (
                        <a
                          href={token.pair_url}
                          target="_blank"
                          rel="noreferrer"
                          className="ml-2 text-xs text-[#F0B90B] hover:text-yellow-300"
                        >
                          Dex
                        </a>
                      )}
                    </td>
                  </tr>
                ))}
                {customTokens.length === 0 && (
                  <tr>
                    <td colSpan={6} className="py-5 text-center text-zinc-500">
                      {language === 'zh' ? '暂无自定义代币数据' : 'No custom token data'}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>

        <section className="rounded-lg border border-cyan-400/20 bg-cyan-400/[0.06] p-5">
          <div className="flex gap-3">
            <ShieldCheck className="mt-0.5 h-5 w-5 shrink-0 text-cyan-300" />
            <div>
              <h2 className="text-sm font-semibold text-cyan-100">
                {language === 'zh' ? '数据源状态' : 'Data Source Status'}
              </h2>
              <p className="mt-2 text-sm leading-6 text-cyan-50/80">
                {health
                  ? `${language === 'zh' ? '最近更新' : 'Last updated'}: ${health.last_updated || '-'} · ${language === 'zh' ? '来源' : 'Source'}: ${health.source || '-'} · ${language === 'zh' ? '真实数据' : 'Real data'}: ${health.real_data ? 'yes' : 'no'}`
                  : loading
                    ? language === 'zh' ? '正在检查数据网关...' : 'Checking data gateway...'
                    : language === 'zh' ? '请确认 nofx-data-gateway 已启动。' : 'Please make sure nofx-data-gateway is running.'}
              </p>
              <div className="mt-3 inline-flex items-center gap-2 text-xs text-cyan-100/80">
                <RefreshCcw className="h-3.5 w-3.5" />
                {language === 'zh' ? '每 30 秒自动刷新' : 'Auto refreshes every 30 seconds'}
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  )
}

function StatusCard({ label, value, tone }: { label: string; value: string; tone: 'good' | 'warn' | 'neutral' }) {
  const color = tone === 'good' ? 'text-emerald-300' : tone === 'warn' ? 'text-amber-300' : 'text-zinc-200'
  return (
    <article className="rounded-lg border border-white/10 bg-white/[0.035] p-4">
      <div className="text-xs uppercase text-zinc-500">{label}</div>
      <div className={`mt-2 text-lg font-semibold ${color}`}>{value}</div>
    </article>
  )
}

function DataList({ icon, title, items }: { icon: ReactNode; title: string; items: Array<{ name: string; value: string }> }) {
  return (
    <article className="rounded-lg border border-white/10 bg-white/[0.035] p-5">
      <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-md border border-[#F0B90B]/25 bg-[#F0B90B]/10 text-[#F0B90B]">
        {icon}
      </div>
      <h2 className="text-base font-semibold text-white">{title}</h2>
      <div className="mt-4 space-y-2">
        {items.length > 0 ? items.map((item) => (
          <div key={`${title}-${item.name}`} className="flex items-center justify-between gap-3 rounded-md bg-black/20 px-3 py-2 text-sm">
            <span className="font-medium text-zinc-100">{item.name}</span>
            <span className="text-right text-xs text-zinc-400">{item.value}</span>
          </div>
        )) : (
          <div className="rounded-md bg-black/20 px-3 py-2 text-sm text-zinc-500">No data</div>
        )}
      </div>
    </article>
  )
}

function formatUSDT(value: number): string {
  const abs = Math.abs(value)
  if (abs >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (abs >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toFixed(2)
}
