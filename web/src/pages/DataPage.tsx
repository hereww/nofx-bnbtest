import type { ReactNode } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { Activity, BarChart3, Database, RefreshCcw, ShieldCheck, TrendingUp } from 'lucide-react'
import { useLanguage } from '../contexts/LanguageContext'
import { t } from '../i18n/translations'

type GatewayHealth = {
  status: string
  service: string
  symbols: number
  snapshot_count: number
  last_refresh: string
  last_refresh_error?: string
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

type PriceRankingResponse = {
  success: boolean
  data?: {
    data?: Record<string, {
      top?: Array<{ pair: string; price_delta: number; price: number }>
      low?: Array<{ pair: string; price_delta: number; price: number }>
    }>
  }
}

export function DataPage() {
  const { language } = useLanguage()
  const [gatewayUrl, setGatewayUrl] = useState('')
  const [health, setHealth] = useState<GatewayHealth | null>(null)
  const [ai500, setAI500] = useState<AI500Response['data'] | null>(null)
  const [oi, setOI] = useState<OIResponse['data'] | null>(null)
  const [netflow, setNetflow] = useState<NetFlowResponse['data'] | null>(null)
  const [priceRanking, setPriceRanking] = useState<PriceRankingResponse['data'] | null>(null)
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

        const [healthRes, ai500Res, oiRes, netflowRes, priceRankingRes] = await Promise.all([
          fetch(`${base}/health`),
          fetch(`${base}/api/ai500/list?limit=6`),
          fetch(`${base}/api/oi/top-ranking?duration=1h&limit=5`),
          fetch(`${base}/api/netflow/top-ranking?duration=1h&limit=5&type=institution&trade=future`),
          fetch(`${base}/api/price/ranking?duration=1h&limit=5`),
        ])
        if (!healthRes.ok) throw new Error(`Gateway health HTTP ${healthRes.status}`)
        const healthJson = (await healthRes.json()) as GatewayHealth
        const ai500Json = ai500Res.ok ? (await ai500Res.json()) as AI500Response : { success: false }
        const oiJson = oiRes.ok ? (await oiRes.json()) as OIResponse : { success: false }
        const netflowJson = netflowRes.ok ? (await netflowRes.json()) as NetFlowResponse : { success: false }
        const priceRankingJson = priceRankingRes.ok
          ? (await priceRankingRes.json()) as PriceRankingResponse
          : { success: false }
        if (!cancelled) {
          setHealth(healthJson)
          setAI500(ai500Json.data || null)
          setOI(oiJson.data || null)
          setNetflow(netflowJson.data || null)
          setPriceRanking(priceRankingJson.data || null)
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
          <StatusCard label="Service" value={health?.service || 'unknown'} tone={health?.service ? 'good' : 'warn'} />
          <StatusCard label="Symbols" value={String(health?.symbols || ai500?.count || 0)} tone="neutral" />
          <StatusCard label="Snapshots" value={String(health?.snapshot_count || 0)} tone="neutral" />
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

        <section className="grid gap-4 lg:grid-cols-2">
          <DataList
            icon={<TrendingUp className="h-5 w-5" />}
            title={language === 'zh' ? '1h 涨幅榜' : '1h Gainers'}
            items={(priceRanking?.data?.['1h']?.top || []).map((item) => ({
              name: item.pair,
              value: `${formatPercent(item.price_delta)} / $${item.price.toFixed(4)}`,
            }))}
          />
          <DataList
            icon={<BarChart3 className="h-5 w-5" />}
            title={language === 'zh' ? '1h 跌幅榜' : '1h Losers'}
            items={(priceRanking?.data?.['1h']?.low || []).map((item) => ({
              name: item.pair,
              value: `${formatPercent(item.price_delta)} / $${item.price.toFixed(4)}`,
            }))}
          />
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
                  ? `${language === 'zh' ? '最近刷新' : 'Last refresh'}: ${formatDateTime(health.last_refresh)} · ${language === 'zh' ? '来源' : 'Source'}: ${health.service || '-'}${health.last_refresh_error ? ` · ${language === 'zh' ? '采集告警' : 'Refresh warning'}: ${health.last_refresh_error}` : ''}`
                  : loading
                    ? language === 'zh' ? '正在检查数据网关...' : 'Checking data gateway...'
                    : language === 'zh' ? '请确认 NOFX 后端或 nofx-data-gateway 已启动。' : 'Please make sure the NOFX backend or nofx-data-gateway is running.'}
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

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(2)}%`
}

function formatDateTime(value?: string): string {
  if (!value || value.startsWith('0001-01-01')) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
