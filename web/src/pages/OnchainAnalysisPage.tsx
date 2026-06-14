import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import {
  Activity,
  Bot,
  Brain,
  Clock3,
  Eye,
  FileText,
  GitBranch,
  Download,
  Layers,
  Network,
  Plus,
  RefreshCcw,
  Search,
  Settings,
  ShieldCheck,
  Target,
  Trash2,
  X,
} from 'lucide-react'
import { useLanguage } from '../contexts/LanguageContext'
import { useAuth } from '../contexts/AuthContext'
import type { AIModel } from '../types'

type WalletAnalysis = {
  address: string
  wallet_type: string
  first_buy_at?: string
  buy_count: number
  sell_count: number
  buy_amount: number
  sell_amount: number
  net_bought_amount: number
  current_balance?: number
  pool_touch_count?: number
  estimated_usd_flow?: number
}

type TimeBucket = {
  bucket: string
  buyer_count: number
  buy_amount: number
  sell_amount: number
  net_amount: number
}

type TokenAnalysis = {
  success: boolean
  chain: string
  address: string
  depth: string
  status: string
  completeness: string
  message?: string
  token?: { name?: string; symbol?: string; price_usd?: string; total_supply?: string }
  security?: {
    holder_count?: number
    is_honeypot?: string
    is_mintable?: string
    transfer_pausable?: string
    slippage_modifiable?: string
    buy_tax?: string
    sell_tax?: string
    top_holders?: Array<{ address: string; tag?: string; balance?: string; percent?: number; is_locked?: boolean }>
  }
  pools?: Array<{ address: string; name?: string; dex_id?: string; liquidity_usd?: number; volume_24h_usd?: number; pair_url?: string }>
  recent?: {
    trade_count: number
    buy_count: number
    sell_count: number
    volume_usd: number
    unique_addresses: number
    first_buy_buckets?: TimeBucket[]
    top_accumulators?: WalletAnalysis[]
    top_sellers?: WalletAnalysis[]
    related_wallet_clusters?: WalletAnalysis[]
  }
  full?: {
    indexed_block?: number
    start_block?: number
    end_block?: number
    wallet_count: number
    first_buy_buckets?: TimeBucket[]
    top_accumulators?: WalletAnalysis[]
    top_sellers?: WalletAnalysis[]
    related_wallet_clusters?: WalletAnalysis[]
  }
  dealer_flow?: DealerFlowAnalysis
  early_wallet_flow?: EarlyWalletFlowResponse
  risk_flags?: string[]
  error?: string
}

type AnalysisDepth = 'recent' | 'full'
type DealerDirection = 'accumulating' | 'distributing' | 'mixed' | 'insufficient_data'
type DealerConfidence = 'low' | 'medium' | 'high'

type DealerFlowAnalysis = {
  direction: DealerDirection
  score: number
  confidence: DealerConfidence
  summary: string
  reasons?: string[]
  monitoring_plan?: string[]
  evidence_depth: string
  signal_breakdown: DealerFlowSignals
}

type DealerFlowSignals = {
  trade_count: number
  buy_count: number
  sell_count: number
  unique_buyers: number
  unique_sellers: number
  wallet_count?: number
  accumulator_count: number
  seller_count: number
  related_wallet_count: number
  accumulator_net_amount: number
  seller_net_amount: number
  net_amount: number
  top_accumulator_net?: number
  top_seller_net?: number
  first_buy_bucket_count?: number
  first_buy_concentration?: number
  top_holder_percent?: number
  top_five_holder_percent?: number
  owner_present?: boolean
  creator_present?: boolean
  high_risk_flag_count?: number
}

type EarlyWalletFlowSummary = {
  direction: DealerDirection
  confidence: DealerConfidence
  seed_wallet_count: number
  tracked_wallet_count: number
  max_observed_depth: number
  total_initial_buy_amount: number
  total_initial_cost: number
  seed_own_sell_amount: number
  seed_own_sell_value: number
  descendant_sell_amount: number
  descendant_sell_value: number
  total_sell_value: number
  realized_pnl: number
  realized_pnl_pct: number
  remaining_amount: number
  remaining_cost: number
  transfer_out_amount: number
  cost_coverage_pct: number
  missing_swap_price_count: number
  incomplete_reasons?: string[]
}

type EarlyWalletSeed = {
  rank: number
  address: string
  first_buy_time?: number
  first_buy_at?: string
  first_buy_block?: number
  buy_count: number
  buy_amount: number
  buy_value: number
  avg_buy_price: number
  own_sell_amount: number
  own_sell_value: number
  transfer_out_amount: number
  current_balance: number
  remaining_cost: number
  realized_pnl: number
  descendant_sell_amount: number
  descendant_sell_value: number
  descendant_realized_pnl: number
  total_realized_pnl: number
  child_count: number
}

type EarlyWalletFlowWallet = {
  address: string
  root_address?: string
  parent_address?: string
  depth: number
  first_seen_time?: number
  first_seen_at?: string
  buy_count: number
  sell_count: number
  buy_amount: number
  buy_value: number
  avg_buy_price: number
  sell_amount: number
  sell_value: number
  transfer_in_amount: number
  transfer_out_amount: number
  current_balance: number
  allocated_cost: number
  remaining_cost: number
  realized_pnl: number
  realized_pnl_pct: number
  cost_coverage_pct: number
  event_count: number
  incomplete_pricing: number
  is_seed: boolean
}

type EarlyWalletFlowEdge = {
  source: string
  target: string
  root_address?: string
  depth: number
  amount: number
  cost?: number
  tx_hash?: string
  block_number?: number
  block_time?: number
  block_at?: string
}

type EarlyWalletFlowResponse = {
  success: boolean
  chain: string
  address: string
  status: string
  completeness: string
  message?: string
  token?: { name?: string; symbol?: string }
  seed_count: number
  max_depth: number
  quote_token?: string
  quote_symbol?: string
  summary: EarlyWalletFlowSummary
  seeds?: EarlyWalletSeed[]
  wallets?: EarlyWalletFlowWallet[]
  edges?: EarlyWalletFlowEdge[]
  error?: string
}

type WalletGraphNode = {
  id: string
  label: string
  node_type: string
  address?: string
  wallet_type?: string
  value?: number
  percent?: number
  buy_count?: number
  sell_count?: number
  buy_amount?: number
  sell_amount?: number
  net_bought_amount?: number
  first_buy_at?: string
}

type WalletGraphEdge = {
  source: string
  target: string
  relation: string
  amount?: number
  weight?: number
  tx_hash?: string
}

type WalletGraphResponse = {
  success: boolean
  chain: string
  address: string
  depth: string
  status: string
  message?: string
  token?: { name?: string; symbol?: string }
  dealer_flow?: DealerFlowAnalysis
  nodes?: WalletGraphNode[]
  edges?: WalletGraphEdge[]
  error?: string
}

type ManagedAnalysisToken = {
  id: string
  chain: 'bsc'
  address: string
  depth: AnalysisDepth
  label?: string
  monitorEnabled: boolean
  monitorIntervalMinutes: number
  analysis?: TokenAnalysis
  aiReport?: OnchainAIReport
  aiReportError?: string
  aiReportLoading?: boolean
  aiReportPrompt?: OnchainAIReportPromptPreview
  aiReportPromptError?: string
  aiReportPromptLoading?: boolean
  walletGraph?: WalletGraphResponse
  walletGraphError?: string
  walletGraphLoading?: boolean
  earlyWalletFlow?: EarlyWalletFlowResponse
  earlyWalletFlowError?: string
  earlyWalletFlowLoading?: boolean
  error?: string
  isLoading?: boolean
  updatedAt?: string
  lastCheckedAt?: string
}

type OnchainAIReport = {
  report: string
  model_id?: string
  model_name?: string
  generated_at: string
}

type OnchainReportStyle = 'balanced' | 'brief' | 'deep' | 'watchlist'
type OnchainReportRiskProfile = 'balanced' | 'defensive' | 'aggressive'

type OnchainReportAgentConfig = {
  modelID: string
  reportStyle: OnchainReportStyle
  riskProfile: OnchainReportRiskProfile
  focus: string
  customPrompt: string
  includeRawSignals: boolean
}

type OnchainAIReportPromptPreview = {
  system_prompt: string
  user_prompt: string
  payload?: string
  config_summary?: Record<string, unknown>
}

type APIErrorBody = {
  error?: string
  error_message?: string
  message?: string
  error_key?: string
  error_params?: Record<string, string>
}

type ForceGraphNode = WalletGraphNode & { id: string }
type ForceGraphLink = WalletGraphEdge & { source: string; target: string }

const ForceGraph3D = lazy(() => import('react-force-graph-3d'))

const ONCHAIN_MANAGER_STORAGE_KEY = 'nofx:onchain-analysis-tokens:v1'
const ONCHAIN_REPORT_AGENT_STORAGE_KEY = 'nofx:onchain-ai-report-agent:v1'
const DEFAULT_ONCHAIN_ADDRESS = '0x812fc5119b772c6c7a66249a559f3614623f4444'
const DEFAULT_MONITOR_INTERVAL_MINUTES = 15
const MIN_MONITOR_INTERVAL_MINUTES = 1
const MAX_MONITOR_INTERVAL_MINUTES = 1440
const MONITOR_TICK_MS = 10_000
const evmAddressPattern = /^0x[0-9a-fA-F]{40}$/

export function OnchainAnalysisPage() {
  const { language } = useLanguage()
  const { token } = useAuth()
  const [analysisTokens, setAnalysisTokens] = useState<ManagedAnalysisToken[]>(() => loadManagedTokens())
  const [aiModels, setAiModels] = useState<AIModel[]>([])
  const [reportAgentConfig, setReportAgentConfig] = useState<OnchainReportAgentConfig>(() => loadReportAgentConfig())
  const [selectedAnalysisID, setSelectedAnalysisID] = useState(() => analysisTokens[0]?.id || '')
  const [newAnalysisAddress, setNewAnalysisAddress] = useState('')
  const [newAnalysisDepth, setNewAnalysisDepth] = useState<AnalysisDepth>('recent')
  const [analysisManagerError, setAnalysisManagerError] = useState('')
  const [reportWindowTokenID, setReportWindowTokenID] = useState('')
  const [graphWindowTokenID, setGraphWindowTokenID] = useState('')

  const selectedToken = analysisTokens.find((token) => token.id === selectedAnalysisID) || analysisTokens[0]
  const selectedAnalysis = selectedToken?.analysis
    ? mergeAnalysisWithWalletGraph(selectedToken.analysis, selectedToken.walletGraph)
    : null
  const anyAnalysisLoading = analysisTokens.some((token) => token.isLoading)
  const reportWindowToken = analysisTokens.find((token) => token.id === reportWindowTokenID) || null
  const graphWindowToken = analysisTokens.find((token) => token.id === graphWindowTokenID) || null

  useEffect(() => {
    localStorage.setItem(ONCHAIN_MANAGER_STORAGE_KEY, JSON.stringify(analysisTokens.map(({
      isLoading: _isLoading,
      aiReportLoading: _aiReportLoading,
      aiReportPromptLoading: _aiReportPromptLoading,
      walletGraphLoading: _walletGraphLoading,
      earlyWalletFlowLoading: _earlyWalletFlowLoading,
      ...token
    }) => token)))
  }, [analysisTokens])

  useEffect(() => {
    localStorage.setItem(ONCHAIN_REPORT_AGENT_STORAGE_KEY, JSON.stringify(reportAgentConfig))
  }, [reportAgentConfig])

  useEffect(() => {
    void fetchAIModels()
  }, [token])

  useEffect(() => {
    if (aiModels.length === 0) return
    if (reportAgentConfig.modelID && aiModels.some((model) => model.id === reportAgentConfig.modelID)) return
    setReportAgentConfig((config) => ({ ...config, modelID: aiModels[0].id }))
  }, [aiModels, reportAgentConfig.modelID])

  useEffect(() => {
    if (analysisTokens.length === 0) {
      setSelectedAnalysisID('')
      return
    }
    if (!analysisTokens.some((token) => token.id === selectedAnalysisID)) {
      setSelectedAnalysisID(analysisTokens[0].id)
    }
  }, [analysisTokens, selectedAnalysisID])

  useEffect(() => {
    function runDueMonitors() {
      const now = Date.now()
      const dueTokens = analysisTokens.filter((token) => {
        if (!token.monitorEnabled || token.isLoading) return false
        const lastCheckedAt = token.lastCheckedAt || token.updatedAt
        if (!lastCheckedAt) return true
        const lastCheckedTime = new Date(lastCheckedAt).getTime()
        if (Number.isNaN(lastCheckedTime)) return true
        return now - lastCheckedTime >= getMonitorIntervalMS(token)
      })

      for (const token of dueTokens) {
        void runTokenAnalysis(token.id)
      }
    }

    const initialTimer = window.setTimeout(runDueMonitors, 1000)
    const timer = window.setInterval(runDueMonitors, MONITOR_TICK_MS)
    return () => {
      window.clearTimeout(initialTimer)
      window.clearInterval(timer)
    }
  }, [analysisTokens])

  async function runTokenAnalysis(tokenID: string, depthOverride?: AnalysisDepth): Promise<TokenAnalysis | undefined> {
    const token = analysisTokens.find((item) => item.id === tokenID)
    if (!token) return undefined
    const depth = depthOverride || token.depth

    setAnalysisManagerError('')
    setAnalysisTokens((items) => items.map((item) => (
      item.id === tokenID ? { ...item, depth, isLoading: true, error: '' } : item
    )))
    try {
      const params = new URLSearchParams({
        chain: token.chain,
        address: token.address,
        depth,
      })
      const res = await fetch(`/api/onchain/token-analysis?${params.toString()}`)
      const data = await readAPIJSON<TokenAnalysis & APIErrorBody>(res)
      if (!res.ok) {
        throw new Error(getAPIErrorMessage(data, `HTTP ${res.status}`))
      }
      if (Object.keys(data).length === 0) {
        throw new Error(language === 'zh' ? '分析接口返回了空响应。' : 'Analysis API returned an empty response.')
      }
      const analysis = data as TokenAnalysis
      const label = analysis.token?.symbol || analysis.token?.name || token.label
      const checkedAt = new Date().toISOString()
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? {
            ...item,
            depth,
            analysis,
            label,
            earlyWalletFlow: analysis.early_wallet_flow || item.earlyWalletFlow,
            error: '',
            isLoading: false,
            updatedAt: checkedAt,
            lastCheckedAt: checkedAt,
          }
          : item
      )))
      return analysis
    } catch (err) {
      const checkedAt = new Date().toISOString()
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? { ...item, error: err instanceof Error ? err.message : 'Analysis failed', isLoading: false, lastCheckedAt: checkedAt }
          : item
      )))
      return undefined
    }
  }

  async function refreshAllAnalyses() {
    for (const token of analysisTokens) {
      await runTokenAnalysis(token.id)
    }
  }

  async function generateAIReport(tokenID: string) {
    const tokenConfig = analysisTokens.find((item) => item.id === tokenID)
    if (!tokenConfig) return

    let analysis = tokenConfig.analysis
    if (!analysis) {
      analysis = await runTokenAnalysis(tokenID)
    }

    if (!analysis) {
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? { ...item, aiReportError: language === 'zh' ? '请先完成链上分析，再生成 AI 报告。' : 'Run on-chain analysis before generating an AI report.' }
          : item
      )))
      setReportWindowTokenID(tokenID)
      return
    }

    setReportWindowTokenID(tokenID)
    setAnalysisTokens((items) => items.map((item) => (
      item.id === tokenID ? { ...item, aiReportLoading: true, aiReportError: '' } : item
    )))
    try {
      let walletGraph = tokenConfig.walletGraph
      if (!walletGraph || walletGraph.depth !== tokenConfig.depth) {
        walletGraph = await loadWalletGraph(tokenID)
      }
      let earlyWalletFlow = tokenConfig.earlyWalletFlow
      if (!earlyWalletFlow) {
        earlyWalletFlow = await loadEarlyWalletFlow(tokenID)
      }
      const analysisForReport = mergeAnalysisWithWalletGraph(analysis, walletGraph)
      const reportAnalysis = mergeAnalysisWithEarlyWalletFlow(analysisForReport, earlyWalletFlow)
      const res = await fetch('/api/onchain/ai-report', {
        method: 'POST',
        headers: getJSONHeaders(token),
        body: JSON.stringify({
          chain: tokenConfig.chain,
          address: tokenConfig.address,
          depth: tokenConfig.depth,
          language,
          analysis: reportAnalysis,
          wallet_graph: walletGraph,
          early_wallet_flow: earlyWalletFlow,
          model_id: getReportModelIDForRequest(reportAgentConfig.modelID, aiModels),
          agent: toReportAgentAPIConfig(reportAgentConfig),
        }),
      })
      const data = await readAPIJSON<OnchainAIReport & APIErrorBody & { success?: boolean }>(res)
      if (!res.ok) {
        throw new Error(getAPIErrorMessage(data, `HTTP ${res.status}`))
      }
      const report = (data as OnchainAIReport).report
      if (!report) {
        throw new Error(language === 'zh' ? 'AI 报告接口返回了空报告。' : 'AI report API returned an empty report.')
      }
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? {
            ...item,
            aiReport: {
              report,
              model_id: (data as OnchainAIReport).model_id,
              model_name: (data as OnchainAIReport).model_name,
              generated_at: (data as OnchainAIReport).generated_at || new Date().toISOString(),
            },
            aiReportLoading: false,
            aiReportError: '',
          }
          : item
      )))
    } catch (err) {
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? { ...item, aiReportLoading: false, aiReportError: err instanceof Error ? err.message : 'AI report failed' }
          : item
      )))
    }
  }

  async function openAIReportWindow(tokenID: string) {
    setReportWindowTokenID(tokenID)
    const tokenConfig = analysisTokens.find((item) => item.id === tokenID)
    if (!tokenConfig) return
    setAnalysisTokens((items) => items.map((item) => (
      item.id === tokenID ? { ...item, aiReportError: '' } : item
    )))
    if (tokenConfig.analysis) return
    await runTokenAnalysis(tokenID)
  }

  async function previewAIReportPrompt(tokenID: string) {
    const tokenConfig = analysisTokens.find((item) => item.id === tokenID)
    if (!tokenConfig) return

    let analysis = tokenConfig.analysis
    if (!analysis) {
      analysis = await runTokenAnalysis(tokenID)
    }
    if (!analysis) {
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? { ...item, aiReportPromptError: language === 'zh' ? '请先完成链上分析，再预览提示词。' : 'Run on-chain analysis before previewing the prompt.' }
          : item
      )))
      setReportWindowTokenID(tokenID)
      return
    }

    setReportWindowTokenID(tokenID)
    setAnalysisTokens((items) => items.map((item) => (
      item.id === tokenID ? { ...item, aiReportPromptLoading: true, aiReportPromptError: '' } : item
    )))

    try {
      let walletGraph = tokenConfig.walletGraph
      if (!walletGraph || walletGraph.depth !== tokenConfig.depth) {
        walletGraph = await loadWalletGraph(tokenID)
      }
      let earlyWalletFlow = tokenConfig.earlyWalletFlow
      if (!earlyWalletFlow) {
        earlyWalletFlow = await loadEarlyWalletFlow(tokenID)
      }
      const analysisForPrompt = mergeAnalysisWithWalletGraph(analysis, walletGraph)
      const promptAnalysis = mergeAnalysisWithEarlyWalletFlow(analysisForPrompt, earlyWalletFlow)
      const res = await fetch('/api/onchain/ai-report/preview', {
        method: 'POST',
        headers: getJSONHeaders(token),
        body: JSON.stringify({
          chain: tokenConfig.chain,
          address: tokenConfig.address,
          depth: tokenConfig.depth,
          language,
          analysis: promptAnalysis,
          wallet_graph: walletGraph,
          early_wallet_flow: earlyWalletFlow,
          model_id: getReportModelIDForRequest(reportAgentConfig.modelID, aiModels),
          agent: toReportAgentAPIConfig(reportAgentConfig),
        }),
      })
      const data = await readAPIJSON<OnchainAIReportPromptPreview & APIErrorBody & { success?: boolean }>(res)
      if (!res.ok) {
        throw new Error(getAPIErrorMessage(data, `HTTP ${res.status}`))
      }
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? {
            ...item,
            aiReportPrompt: {
              system_prompt: String((data as OnchainAIReportPromptPreview).system_prompt || ''),
              user_prompt: String((data as OnchainAIReportPromptPreview).user_prompt || ''),
              payload: (data as OnchainAIReportPromptPreview).payload,
              config_summary: (data as OnchainAIReportPromptPreview).config_summary,
            },
            aiReportPromptLoading: false,
            aiReportPromptError: '',
          }
          : item
      )))
    } catch (err) {
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? { ...item, aiReportPromptLoading: false, aiReportPromptError: err instanceof Error ? err.message : 'Prompt preview failed' }
          : item
      )))
    }
  }

  async function fetchAIModels() {
    try {
      const res = await fetch('/api/models', {
        headers: getJSONHeaders(token),
      })
      if (!res.ok) {
        setAiModels([])
        return
      }
      const data = await readAPIJSON<AIModel[] | ({ models?: AIModel[] } & APIErrorBody)>(res)
      const models = (Array.isArray(data) ? data : ((data as { models?: AIModel[] }).models || []))
        .filter((model): model is AIModel => Boolean(model))
      setAiModels(models.filter((model) => model.enabled))
    } catch {
      setAiModels([])
    }
  }

  async function startFullIndex(tokenID: string) {
    const tokenConfig = analysisTokens.find((item) => item.id === tokenID)
    if (!tokenConfig) return

    setAnalysisManagerError('')
    setAnalysisTokens((items) => items.map((item) => (
      item.id === tokenID ? { ...item, isLoading: true, error: '' } : item
    )))
    try {
      const token = localStorage.getItem('auth_token') || ''
      const res = await fetch('/api/onchain/index-token', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({ chain: tokenConfig.chain, address: tokenConfig.address }),
      })
      const data = await readAPIJSON<APIErrorBody>(res)
      if (!res.ok) {
        throw new Error(getAPIErrorMessage(data, `HTTP ${res.status}`))
      }
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID ? { ...item, depth: 'full', isLoading: false } : item
      )))
      await runTokenAnalysis(tokenID, 'full')
    } catch (err) {
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? { ...item, error: err instanceof Error ? err.message : 'Index request failed', isLoading: false }
          : item
      )))
    }
  }

  async function startEarlyWalletFlowIndex(tokenID: string) {
    const tokenConfig = analysisTokens.find((item) => item.id === tokenID)
    if (!tokenConfig) return

    setAnalysisManagerError('')
    setAnalysisTokens((items) => items.map((item) => (
      item.id === tokenID ? { ...item, earlyWalletFlowLoading: true, earlyWalletFlowError: '' } : item
    )))
    try {
      const res = await fetch('/api/onchain/early-wallet-flow/index', {
        method: 'POST',
        headers: getJSONHeaders(token),
        body: JSON.stringify({ chain: tokenConfig.chain, address: tokenConfig.address }),
      })
      const data = await readAPIJSON<APIErrorBody>(res)
      if (!res.ok) {
        throw new Error(getAPIErrorMessage(data, `HTTP ${res.status}`))
      }
      await loadEarlyWalletFlow(tokenID)
    } catch (err) {
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? { ...item, earlyWalletFlowLoading: false, earlyWalletFlowError: err instanceof Error ? err.message : 'Early wallet flow index failed' }
          : item
      )))
    }
  }

  async function loadEarlyWalletFlow(tokenID: string): Promise<EarlyWalletFlowResponse | undefined> {
    const tokenConfig = analysisTokens.find((item) => item.id === tokenID)
    if (!tokenConfig) return undefined
    setAnalysisTokens((items) => items.map((item) => (
      item.id === tokenID ? { ...item, earlyWalletFlowLoading: true, earlyWalletFlowError: '' } : item
    )))
    try {
      const params = new URLSearchParams({
        chain: tokenConfig.chain,
        address: tokenConfig.address,
        seed_count: '100',
        max_depth: '4',
      })
      const res = await fetch(`/api/onchain/early-wallet-flow?${params.toString()}`)
      const data = await readAPIJSON<EarlyWalletFlowResponse & APIErrorBody>(res)
      if (!res.ok) {
        throw new Error(getAPIErrorMessage(data, `HTTP ${res.status}`))
      }
      if (Object.keys(data).length === 0) {
        throw new Error(language === 'zh' ? '早期地址资金流接口返回了空响应。' : 'Early wallet flow API returned an empty response.')
      }
      const flow = data as EarlyWalletFlowResponse
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? {
            ...item,
            label: flow.token?.symbol || flow.token?.name || item.label,
            analysis: mergeAnalysisWithEarlyWalletFlow(item.analysis, flow),
            earlyWalletFlow: flow,
            earlyWalletFlowLoading: false,
            earlyWalletFlowError: '',
          }
          : item
      )))
      return flow
    } catch (err) {
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? { ...item, earlyWalletFlowLoading: false, earlyWalletFlowError: err instanceof Error ? err.message : 'Early wallet flow failed' }
          : item
      )))
      return undefined
    }
  }

  function exportEarlyWalletFlow(tokenID: string) {
    const tokenConfig = analysisTokens.find((item) => item.id === tokenID)
    if (!tokenConfig) return
    const params = new URLSearchParams({
      chain: tokenConfig.chain,
      address: tokenConfig.address,
      seed_count: '100',
      max_depth: '4',
    })
    window.location.href = `/api/onchain/early-wallet-flow/export?${params.toString()}`
  }

  async function loadWalletGraph(tokenID: string): Promise<WalletGraphResponse | undefined> {
    const tokenConfig = analysisTokens.find((item) => item.id === tokenID)
    if (!tokenConfig) return undefined
    setAnalysisTokens((items) => items.map((item) => (
      item.id === tokenID ? { ...item, walletGraphLoading: true, walletGraphError: '' } : item
    )))
    try {
      const params = new URLSearchParams({
        chain: tokenConfig.chain,
        address: tokenConfig.address,
        depth: tokenConfig.depth,
        limit: '80',
      })
      const res = await fetch(`/api/onchain/wallet-graph?${params.toString()}`)
      const data = await readAPIJSON<WalletGraphResponse & APIErrorBody>(res)
      if (!res.ok) {
        throw new Error(getAPIErrorMessage(data, `HTTP ${res.status}`))
      }
      if (Object.keys(data).length === 0) {
        throw new Error(language === 'zh' ? '关系网接口返回了空响应。' : 'Wallet graph API returned an empty response.')
      }
      const graph = data as WalletGraphResponse
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? {
            ...item,
            label: graph.token?.symbol || graph.token?.name || item.label,
            analysis: mergeAnalysisWithWalletGraph(item.analysis, graph),
            walletGraph: graph,
            walletGraphLoading: false,
            walletGraphError: '',
          }
          : item
      )))
      return graph
    } catch (err) {
      setAnalysisTokens((items) => items.map((item) => (
        item.id === tokenID
          ? { ...item, walletGraphLoading: false, walletGraphError: err instanceof Error ? err.message : 'Wallet graph failed' }
          : item
      )))
      return undefined
    }
  }

  async function openWalletGraph(tokenID: string) {
    setGraphWindowTokenID(tokenID)
    const tokenConfig = analysisTokens.find((item) => item.id === tokenID)
    if (!tokenConfig?.walletGraph) {
      await loadWalletGraph(tokenID)
    }
  }

  function addAnalysisToken() {
    const address = normalizeEVMAddress(newAnalysisAddress)
    if (!evmAddressPattern.test(address)) {
      setAnalysisManagerError(language === 'zh' ? '请输入有效的 EVM 合约地址。' : 'Enter a valid EVM contract address.')
      return
    }
    if (analysisTokens.some((token) => token.address === address)) {
      const existing = analysisTokens.find((token) => token.address === address)
      if (existing) setSelectedAnalysisID(existing.id)
      setAnalysisManagerError(language === 'zh' ? '这个合约已经在管理器里。' : 'This contract is already in the manager.')
      return
    }
    const next: ManagedAnalysisToken = {
      id: `bsc:${address}`,
      chain: 'bsc',
      address,
      depth: newAnalysisDepth,
      monitorEnabled: true,
      monitorIntervalMinutes: DEFAULT_MONITOR_INTERVAL_MINUTES,
    }
    setAnalysisTokens((items) => [...items, next])
    setSelectedAnalysisID(next.id)
    setNewAnalysisAddress('')
    setAnalysisManagerError('')
  }

  function updateTokenDepth(tokenID: string, depth: AnalysisDepth) {
    setAnalysisTokens((items) => items.map((token) => (
      token.id === tokenID ? { ...token, depth } : token
    )))
  }

  function updateTokenMonitorEnabled(tokenID: string, enabled: boolean) {
    setAnalysisTokens((items) => items.map((token) => (
      token.id === tokenID ? { ...token, monitorEnabled: enabled } : token
    )))
  }

  function updateTokenMonitorInterval(tokenID: string, minutes: number) {
    setAnalysisTokens((items) => items.map((token) => (
      token.id === tokenID ? { ...token, monitorIntervalMinutes: clampMonitorIntervalMinutes(minutes) } : token
    )))
  }

  function removeAnalysisToken(tokenID: string) {
    setAnalysisTokens((items) => items.filter((token) => token.id !== tokenID))
  }

  return (
    <div className="min-h-[calc(100vh-64px)] bg-[#05070d] text-white">
      <div className="flex w-full flex-col gap-4 px-3 py-4 sm:px-4 lg:px-6 2xl:px-8">
        <section className="rounded-lg border border-white/10 bg-white/[0.03] px-4 py-3 shadow-xl shadow-black/20 sm:px-5">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="min-w-0">
              <div className="mb-2 inline-flex items-center gap-2 rounded-full border border-[#F0B90B]/30 bg-[#F0B90B]/10 px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wide text-[#F0B90B]">
                <ShieldCheck className="h-3.5 w-3.5" />
                {language === 'zh' ? '链上分析模块' : 'On-chain module'}
              </div>
              <h1 className="text-2xl font-semibold tracking-normal text-white sm:text-3xl">
                {language === 'zh' ? '链上分析' : 'On-chain Analysis'}
              </h1>
              <p className="mt-2 max-w-4xl text-sm leading-6 text-zinc-300">
                {language === 'zh'
                  ? '左侧管理合约和监控间隔，右侧查看选中币种的分析结果。每个 BSC 合约都可以独立设置 recent/full 深度和自动监控时间。'
                  : 'Manage contracts and monitor intervals on the left, then review the selected token analysis on the right. Each BSC contract can use its own recent/full depth and auto-monitor interval.'}
              </p>
            </div>
            <button
              type="button"
              onClick={refreshAllAnalyses}
              disabled={anyAnalysisLoading || analysisTokens.length === 0}
              className="inline-flex items-center justify-center gap-2 rounded-md border border-[#F0B90B]/25 bg-[#F0B90B]/10 px-4 py-2 text-sm font-semibold text-[#F0B90B] disabled:opacity-50"
            >
              <RefreshCcw className="h-4 w-4" />
              {language === 'zh' ? '刷新全部' : 'Refresh all'}
            </button>
          </div>
        </section>

        <section className="grid min-w-0 items-start gap-4 xl:grid-cols-[340px_minmax(0,1fr)] 2xl:grid-cols-[380px_minmax(0,1fr)]">
          <div className="min-w-0 space-y-3 xl:sticky xl:top-4 xl:max-h-[calc(100vh-96px)] xl:overflow-y-auto xl:pr-1">
            <div className="rounded-lg border border-white/10 bg-white/[0.035] p-4">
              <div className="grid gap-2">
                <input
                  value={newAnalysisAddress}
                  onChange={(event) => setNewAnalysisAddress(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') addAnalysisToken()
                  }}
                  className="min-w-0 rounded-md border border-white/10 bg-black/30 px-3 py-2 font-mono text-xs text-zinc-100 outline-none focus:border-[#F0B90B]/60"
                  placeholder="0x..."
                />
                <div className="grid grid-cols-[1fr_auto] gap-2">
                  <select
                    value={newAnalysisDepth}
                    onChange={(event) => setNewAnalysisDepth(event.target.value as AnalysisDepth)}
                    className="min-w-0 rounded-md border border-white/10 bg-black/30 px-3 py-2 text-sm text-zinc-100 outline-none focus:border-[#F0B90B]/60"
                  >
                    <option value="recent">recent</option>
                    <option value="full">full</option>
                  </select>
                  <button
                    type="button"
                    onClick={addAnalysisToken}
                    className="inline-flex items-center justify-center gap-2 rounded-md bg-[#F0B90B] px-4 py-2 text-sm font-semibold text-black"
                  >
                    <Plus className="h-4 w-4" />
                    {language === 'zh' ? '添加' : 'Add'}
                  </button>
                </div>
              </div>
              {analysisManagerError && (
                <div className="mt-3 rounded-md border border-amber-300/25 bg-amber-300/10 px-3 py-2 text-xs text-amber-100">
                  {analysisManagerError}
                </div>
              )}
            </div>

            <div className="space-y-2">
              {analysisTokens.map((token) => {
                const dealerFlow = token.analysis?.dealer_flow || token.walletGraph?.dealer_flow
                return (
                  <button
                    key={token.id}
                    type="button"
                    onClick={() => setSelectedAnalysisID(token.id)}
                    className={`w-full rounded-lg border p-3 text-left transition ${selectedToken?.id === token.id ? 'border-[#F0B90B]/45 bg-[#F0B90B]/10' : 'border-white/10 bg-white/[0.035] hover:border-white/20'}`}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-sm font-semibold text-zinc-100">
                            {token.label || token.analysis?.token?.symbol || shortenAddress(token.address)}
                          </span>
                          {token.isLoading && <span className="text-xs text-[#F0B90B]">{language === 'zh' ? '刷新中' : 'Loading'}</span>}
                        </div>
                        <div className="mt-1 truncate font-mono text-xs text-zinc-500">{token.address}</div>
                      </div>
                      <span className="shrink-0 rounded bg-white/10 px-2 py-1 text-xs text-zinc-300">{token.depth}</span>
                    </div>
                    <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-zinc-400">
                      <span>{token.analysis?.status || token.walletGraph?.status || '-'}</span>
                      <span>{getDealerDirectionLabel(dealerFlow?.direction, language)}</span>
                      <span className={`text-right ${getDealerToneClass(dealerFlow?.direction)}`}>
                        {typeof dealerFlow?.score === 'number' ? formatSigned(dealerFlow.score) : '-'}
                      </span>
                    </div>
                    <div className="mt-2 flex items-center justify-between gap-2 text-xs text-zinc-500">
                      <span className={token.monitorEnabled ? 'text-emerald-300' : 'text-zinc-500'}>
                        {token.monitorEnabled
                          ? `${language === 'zh' ? '监控' : 'Monitor'} ${formatMonitorInterval(token.monitorIntervalMinutes)}`
                          : (language === 'zh' ? '监控关闭' : 'Monitor off')}
                      </span>
                      <span>{language === 'zh' ? '下次' : 'Next'} {formatNextMonitorTime(token)}</span>
                    </div>
                    {token.error && <div className="mt-2 break-words text-xs text-red-200">{token.error}</div>}
                  </button>
                )
              })}
              {analysisTokens.length === 0 && (
                <div className="rounded-lg border border-dashed border-white/10 bg-white/[0.035] p-5 text-center text-sm text-zinc-500">
                  {language === 'zh' ? '还没有添加任何合约' : 'No contracts added yet'}
                </div>
              )}
            </div>
          </div>

          <div className="min-w-0 space-y-4">
            {selectedToken ? (
              <>
                <div className="rounded-lg border border-white/10 bg-white/[0.035] p-4">
                  <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <h2 className="text-lg font-semibold text-white">
                          {selectedAnalysis?.token?.symbol || selectedToken.label || shortenAddress(selectedToken.address)}
                        </h2>
                        <span className="rounded bg-white/10 px-2 py-1 text-xs text-zinc-200">
                          {selectedAnalysis?.status || 'not_analyzed'}
                        </span>
                      </div>
                      <div className="mt-1 break-all font-mono text-xs text-zinc-500">{selectedToken.address}</div>
                      {selectedAnalysis?.token?.name && <div className="mt-1 text-sm text-zinc-400">{selectedAnalysis.token.name}</div>}
                    </div>
                    <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:justify-end">
                      <select
                        value={selectedToken.depth}
                        onChange={(event) => updateTokenDepth(selectedToken.id, event.target.value as AnalysisDepth)}
                        className="rounded-md border border-white/10 bg-black/30 px-3 py-2 text-sm text-zinc-100 outline-none focus:border-[#F0B90B]/60"
                      >
                        <option value="recent">recent</option>
                        <option value="full">full</option>
                      </select>
                      <button
                        type="button"
                        onClick={() => runTokenAnalysis(selectedToken.id)}
                        disabled={selectedToken.isLoading}
                        className="inline-flex items-center justify-center gap-2 rounded-md bg-[#F0B90B] px-4 py-2 text-sm font-semibold text-black disabled:opacity-60"
                      >
                        <Search className="h-4 w-4" />
                        {selectedToken.isLoading ? (language === 'zh' ? '分析中' : 'Analyzing') : (language === 'zh' ? '分析' : 'Analyze')}
                      </button>
                      <button
                        type="button"
                        onClick={() => openWalletGraph(selectedToken.id)}
                        disabled={selectedToken.walletGraphLoading}
                        className="inline-flex items-center justify-center gap-2 rounded-md border border-emerald-300/25 bg-emerald-300/10 px-4 py-2 text-sm font-semibold text-emerald-100 disabled:opacity-60"
                      >
                        <Network className="h-4 w-4" />
                        {selectedToken.walletGraphLoading ? (language === 'zh' ? '加载中' : 'Loading') : (language === 'zh' ? '关系网' : 'Graph')}
                      </button>
                      <button
                        type="button"
                        onClick={() => openAIReportWindow(selectedToken.id)}
                        disabled={selectedToken.isLoading || selectedToken.aiReportLoading}
                        className="inline-flex items-center justify-center gap-2 rounded-md border border-violet-300/25 bg-violet-300/10 px-4 py-2 text-sm font-semibold text-violet-100 disabled:opacity-60"
                      >
                        <Brain className="h-4 w-4" />
                        {selectedToken.aiReportLoading ? (language === 'zh' ? '生成中' : 'Writing') : (language === 'zh' ? 'AI 报告' : 'AI Report')}
                      </button>
                      <button
                        type="button"
                        onClick={() => removeAnalysisToken(selectedToken.id)}
                        className="inline-flex items-center justify-center rounded-md border border-red-300/20 bg-red-400/10 px-3 py-2 text-sm text-red-100"
                        aria-label={language === 'zh' ? '删除合约' : 'Remove contract'}
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  </div>
                  <div className="mt-4 grid gap-3 border-t border-white/10 pt-4 lg:grid-cols-[minmax(0,1fr)_160px_140px_140px] lg:items-end">
                    <label className="flex items-center justify-between gap-3 rounded-md border border-white/10 bg-black/20 px-3 py-2">
                      <span className="inline-flex items-center gap-2 text-sm font-medium text-zinc-100">
                        <Clock3 className="h-4 w-4 text-[#F0B90B]" />
                        {language === 'zh' ? '自动监控' : 'Auto monitor'}
                      </span>
                      <input
                        type="checkbox"
                        checked={selectedToken.monitorEnabled}
                        onChange={(event) => updateTokenMonitorEnabled(selectedToken.id, event.target.checked)}
                        className="h-4 w-4 accent-[#F0B90B]"
                      />
                    </label>
                    <label className="block">
                      <span className="mb-1 block text-xs text-zinc-500">
                        {language === 'zh' ? '间隔（分钟）' : 'Interval minutes'}
                      </span>
                      <input
                        type="number"
                        min={MIN_MONITOR_INTERVAL_MINUTES}
                        max={MAX_MONITOR_INTERVAL_MINUTES}
                        value={selectedToken.monitorIntervalMinutes}
                        onChange={(event) => updateTokenMonitorInterval(selectedToken.id, event.target.valueAsNumber)}
                        className="w-full rounded-md border border-white/10 bg-black/30 px-3 py-2 text-sm text-zinc-100 outline-none focus:border-[#F0B90B]/60"
                      />
                    </label>
                    <Metric label={language === 'zh' ? '上次检查' : 'Last check'} value={formatNullableTime(selectedToken.lastCheckedAt || selectedToken.updatedAt)} />
                    <Metric label={language === 'zh' ? '下次监控' : 'Next run'} value={formatNextMonitorTime(selectedToken)} />
                  </div>
                  {selectedToken.error && (
                    <div className="mt-4 rounded-md border border-red-400/25 bg-red-400/[0.06] px-3 py-2 text-sm text-red-100">
                      {selectedToken.error}
                    </div>
                  )}
                  {selectedToken.aiReportError && (
                    <div className="mt-4 rounded-md border border-violet-300/25 bg-violet-300/[0.08] px-3 py-2 text-sm text-violet-100">
                      {selectedToken.aiReportError}
                    </div>
                  )}
                  {selectedToken.earlyWalletFlowError && (
                    <div className="mt-4 rounded-md border border-cyan-300/25 bg-cyan-300/[0.08] px-3 py-2 text-sm text-cyan-100">
                      {selectedToken.earlyWalletFlowError}
                    </div>
                  )}
                  {selectedToken.walletGraphError && (
                    <div className="mt-4 rounded-md border border-emerald-300/25 bg-emerald-300/[0.08] px-3 py-2 text-sm text-emerald-100">
                      {selectedToken.walletGraphError}
                    </div>
                  )}
                  {selectedAnalysis?.message && <p className="mt-3 text-sm text-amber-100">{selectedAnalysis.message}</p>}
                </div>

                <EarlyWalletFlowPanel
                  flow={selectedToken.earlyWalletFlow || selectedAnalysis?.early_wallet_flow}
                  language={language}
                  loading={Boolean(selectedToken.earlyWalletFlowLoading)}
                  onLoad={() => loadEarlyWalletFlow(selectedToken.id)}
                  onStartIndex={() => startEarlyWalletFlowIndex(selectedToken.id)}
                  onExport={() => exportEarlyWalletFlow(selectedToken.id)}
                />

                {selectedAnalysis ? (
                  <section className="rounded-lg border border-[#F0B90B]/20 bg-[#F0B90B]/[0.04] p-4 2xl:p-5">
                    <DealerFlowPanel
                      analysis={selectedAnalysis}
                      language={language}
                      onOpenGraph={() => openWalletGraph(selectedToken.id)}
                      onStartFullIndex={() => startFullIndex(selectedToken.id)}
                      graphLoading={Boolean(selectedToken.walletGraphLoading)}
                      indexLoading={Boolean(selectedToken.isLoading)}
                    />
                    <div className="mb-4 flex flex-col gap-2 border-b border-[#F0B90B]/15 pb-4 sm:flex-row sm:items-end sm:justify-between">
                      <div>
                        <h2 className="text-lg font-semibold text-white">
                          {language === 'zh' ? '链上明细' : 'On-chain Details'}
                        </h2>
                        <p className="mt-1 text-sm text-zinc-400">
                          {selectedAnalysis.token?.symbol || selectedToken.label || shortenAddress(selectedToken.address)} · {selectedAnalysis.status} · {formatNullableTime(selectedToken.updatedAt)}
                        </p>
                      </div>
                      <span className="inline-flex w-fit items-center rounded-md border border-white/10 bg-black/20 px-3 py-1.5 text-xs text-zinc-300">
                        {selectedToken.monitorEnabled
                          ? `${language === 'zh' ? '自动监控' : 'Auto'} ${formatMonitorInterval(selectedToken.monitorIntervalMinutes)}`
                          : (language === 'zh' ? '手动分析' : 'Manual')}
                      </span>
                      <button
                        type="button"
                        onClick={() => startFullIndex(selectedToken.id)}
                        disabled={selectedToken.isLoading}
                        className="inline-flex w-fit items-center gap-2 rounded-md border border-cyan-300/25 bg-cyan-300/10 px-3 py-1.5 text-xs font-semibold text-cyan-100 disabled:opacity-60"
                      >
                        <GitBranch className="h-3.5 w-3.5" />
                        {language === 'zh' ? '启动 full 索引' : 'Start full index'}
                      </button>
                      <button
                        type="button"
                        onClick={() => openAIReportWindow(selectedToken.id)}
                        disabled={selectedToken.aiReportLoading}
                        className="inline-flex w-fit items-center gap-2 rounded-md border border-violet-300/25 bg-violet-300/10 px-3 py-1.5 text-xs font-semibold text-violet-100 disabled:opacity-60"
                      >
                        <Brain className="h-3.5 w-3.5" />
                        {selectedToken.aiReportLoading ? (language === 'zh' ? '生成报告中' : 'Writing report') : (language === 'zh' ? '查看 AI 报告' : 'View AI report')}
                      </button>
                      <button
                        type="button"
                        onClick={() => loadEarlyWalletFlow(selectedToken.id)}
                        disabled={selectedToken.earlyWalletFlowLoading}
                        className="inline-flex w-fit items-center gap-2 rounded-md border border-orange-300/25 bg-orange-300/10 px-3 py-1.5 text-xs font-semibold text-orange-100 disabled:opacity-60"
                      >
                        <Layers className="h-3.5 w-3.5" />
                        {selectedToken.earlyWalletFlowLoading ? (language === 'zh' ? '计算中' : 'Calculating') : (language === 'zh' ? '早期资金流' : 'Early flow')}
                      </button>
                      <button
                        type="button"
                        onClick={() => exportEarlyWalletFlow(selectedToken.id)}
                        className="inline-flex w-fit items-center gap-2 rounded-md border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-semibold text-zinc-100"
                      >
                        <Download className="h-3.5 w-3.5" />
                        CSV
                      </button>
                    </div>

                    <div className="grid gap-4 2xl:grid-cols-[360px_minmax(0,1fr)]">
                      <div className="min-w-0 space-y-4">
                        <div className="rounded-lg border border-white/10 bg-white/[0.035] p-4">
                          <div className="grid grid-cols-2 gap-3 text-sm">
                            <Metric label="Depth" value={selectedAnalysis.depth} />
                            <Metric label="Completeness" value={selectedAnalysis.completeness} />
                            <Metric label="Price" value={selectedAnalysis.token?.price_usd ? `$${selectedAnalysis.token.price_usd}` : '-'} />
                            <Metric label="Holders" value={String(selectedAnalysis.security?.holder_count || '-')} />
                            <Metric label="Buy tax" value={selectedAnalysis.security?.buy_tax || '-'} />
                            <Metric label="Sell tax" value={selectedAnalysis.security?.sell_tax || '-'} />
                          </div>
                          {(selectedAnalysis.risk_flags || []).length > 0 && (
                            <div className="mt-4 flex flex-wrap gap-2">
                              {selectedAnalysis.risk_flags?.map((flag) => (
                                <span key={flag} className="rounded bg-amber-300/10 px-2 py-1 text-xs text-amber-100">{flag}</span>
                              ))}
                            </div>
                          )}
                        </div>

                        <MiniList title={language === 'zh' ? '主要池子' : 'Pools'} items={(selectedAnalysis.pools || []).slice(0, 5).map((pool) => ({
                          name: pool.name || pool.address,
                          value: `${formatUSDT(pool.liquidity_usd || 0)} liq / ${formatUSDT(pool.volume_24h_usd || 0)} vol`,
                        }))} />

                        <MiniList title="Top holders" items={(selectedAnalysis.security?.top_holders || []).slice(0, 8).map((holder) => ({
                          name: holder.tag || shortenAddress(holder.address),
                          value: `${((holder.percent || 0) * 100).toFixed(2)}%`,
                        }))} />
                      </div>

                      <div className="min-w-0 space-y-4">
                        <div className="grid gap-3 sm:grid-cols-4">
                          <MetricCard label="Trades" value={String(selectedAnalysis.recent?.trade_count || 0)} />
                          <MetricCard label="Buys" value={String(selectedAnalysis.recent?.buy_count || 0)} />
                          <MetricCard label="Sells" value={String(selectedAnalysis.recent?.sell_count || 0)} />
                          <MetricCard label="Volume" value={formatUSDT(selectedAnalysis.recent?.volume_usd || 0)} />
                        </div>
                        <DealerCharts analysis={selectedAnalysis} language={language} />
                        <WalletTable title={language === 'zh' ? '净买入地址' : 'Top accumulators'} wallets={selectedAnalysis.full?.top_accumulators || selectedAnalysis.recent?.top_accumulators || []} />
                        <WalletTable title={language === 'zh' ? '主要卖出地址' : 'Top sellers'} wallets={selectedAnalysis.full?.top_sellers || selectedAnalysis.recent?.top_sellers || []} />
                        <WalletTable title={language === 'zh' ? '关联/套利地址' : 'Related / arbitrage wallets'} wallets={selectedAnalysis.full?.related_wallet_clusters || selectedAnalysis.recent?.related_wallet_clusters || []} />
                        <MiniList title={language === 'zh' ? '首次买入时间分布' : 'First buy buckets'} items={(selectedAnalysis.full?.first_buy_buckets || selectedAnalysis.recent?.first_buy_buckets || []).slice(-8).map((bucket) => ({
                          name: bucket.bucket,
                          value: `${bucket.buyer_count} buyers / net ${formatTokenAmount(bucket.net_amount)}`,
                        }))} />
                      </div>
                    </div>
                  </section>
                ) : (
                  <div className="rounded-lg border border-dashed border-[#F0B90B]/25 bg-[#F0B90B]/[0.04] p-8 text-center">
                    <h2 className="text-lg font-semibold text-white">
                      {language === 'zh' ? '分析结果会显示在这里' : 'Analysis results appear here'}
                    </h2>
                    <p className="mx-auto mt-2 max-w-md text-sm leading-6 text-zinc-400">
                      {language === 'zh' ? '点击上方“分析”，或开启自动监控后等待下一次检查。结果包括安全画像、交易行为、钱包分布和首次买入时间分布。' : 'Click Analyze above, or enable auto monitor and wait for the next check. Results include security, trading behavior, wallet distribution, and first-buy buckets.'}
                    </p>
                    <button
                      type="button"
                      onClick={() => runTokenAnalysis(selectedToken.id)}
                      disabled={selectedToken.isLoading}
                      className="mt-5 inline-flex items-center justify-center gap-2 rounded-md bg-[#F0B90B] px-4 py-2 text-sm font-semibold text-black disabled:opacity-60"
                    >
                      <Search className="h-4 w-4" />
                      {selectedToken.isLoading ? (language === 'zh' ? '分析中' : 'Analyzing') : (language === 'zh' ? '立即分析' : 'Analyze now')}
                    </button>
                  </div>
                )}
              </>
            ) : (
              <div className="rounded-lg border border-dashed border-white/10 bg-white/[0.035] p-8 text-center text-sm text-zinc-500">
                {language === 'zh' ? '添加一个 BSC 合约开始管理链上分析。' : 'Add a BSC contract to start managing on-chain analysis.'}
              </div>
            )}
          </div>
        </section>
      </div>
      {reportWindowToken && (
        <AIReportWindow
          token={reportWindowToken}
          language={language}
          agentConfig={reportAgentConfig}
          aiModels={aiModels}
          onAgentConfigChange={setReportAgentConfig}
          onClose={() => setReportWindowTokenID('')}
          onGenerate={() => generateAIReport(reportWindowToken.id)}
          onPreviewPrompt={() => previewAIReportPrompt(reportWindowToken.id)}
        />
      )}
      {graphWindowToken && (
        <WalletGraphWindow
          token={graphWindowToken}
          language={language}
          onClose={() => setGraphWindowTokenID('')}
          onRefresh={() => loadWalletGraph(graphWindowToken.id)}
        />
      )}
    </div>
  )
}

async function readAPIJSON<T extends object>(res: Response): Promise<Partial<T>> {
  const text = await res.text()
  const trimmed = text.trim()
  if (!trimmed) return {}
  try {
    return JSON.parse(trimmed) as T
  } catch {
    const preview = trimmed.replace(/\s+/g, ' ').slice(0, 160)
    throw new Error(`HTTP ${res.status}: invalid JSON response${preview ? ` (${preview})` : ''}`)
  }
}

function getAPIErrorMessage(data: Partial<APIErrorBody>, fallback: string): string {
  const base = data.error_message || data.error || data.message || fallback
  const reason = data.error_params?.reason
  if (reason === 'upstream_returned_html') {
    return `${base}${base.includes('/v1') ? '' : ' 请把 Base URL 改成 OpenAI 兼容地址，通常需要以 /v1 结尾。'}`
  }
  if (reason === 'api_key_invalid_or_missing') {
    return `${base} 如果你刚刚配置过模型，请重新保存一次 API Key。`
  }
  return base
}

function getJSONHeaders(token: string | null): Record<string, string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  return headers
}

function toReportAgentAPIConfig(config: OnchainReportAgentConfig) {
  return {
    report_style: config.reportStyle,
    risk_profile: config.riskProfile,
    focus: config.focus,
    custom_prompt: config.customPrompt,
    include_raw_signals: config.includeRawSignals,
  }
}

function getReportModelIDForRequest(modelID: string, aiModels: AIModel[]): string {
  const cleanModelID = modelID.trim()
  if (cleanModelID && aiModels.some((model) => model.id === cleanModelID)) {
    return cleanModelID
  }
  return ''
}

function getSelectedReportModelName(modelID: string, aiModels: AIModel[], language: string): string {
  const cleanModelID = modelID.trim()
  if (!cleanModelID) {
    return language === 'zh' ? '自动选择可用模型' : 'Auto select enabled model'
  }
  const model = aiModels.find((item) => item.id === cleanModelID)
  if (model) return model.name
  return language === 'zh' ? '自动选择可用模型（原模型不可用）' : 'Auto select enabled model (saved model unavailable)'
}

function loadReportAgentConfig(): OnchainReportAgentConfig {
  if (typeof window === 'undefined') return defaultReportAgentConfig()
  try {
    const raw = localStorage.getItem(ONCHAIN_REPORT_AGENT_STORAGE_KEY)
    if (!raw) return defaultReportAgentConfig()
    const parsed = JSON.parse(raw) as Partial<OnchainReportAgentConfig>
    return {
      ...defaultReportAgentConfig(),
      ...parsed,
      reportStyle: isReportStyle(parsed.reportStyle) ? parsed.reportStyle : 'balanced',
      riskProfile: isRiskProfile(parsed.riskProfile) ? parsed.riskProfile : 'balanced',
      focus: typeof parsed.focus === 'string' ? parsed.focus : '',
      customPrompt: typeof parsed.customPrompt === 'string' ? parsed.customPrompt : '',
      includeRawSignals: Boolean(parsed.includeRawSignals),
    }
  } catch {
    return defaultReportAgentConfig()
  }
}

function defaultReportAgentConfig(): OnchainReportAgentConfig {
  return {
    modelID: '',
    reportStyle: 'balanced',
    riskProfile: 'balanced',
    focus: '',
    customPrompt: '',
    includeRawSignals: false,
  }
}

function isReportStyle(value: unknown): value is OnchainReportStyle {
  return value === 'balanced' || value === 'brief' || value === 'deep' || value === 'watchlist'
}

function isRiskProfile(value: unknown): value is OnchainReportRiskProfile {
  return value === 'balanced' || value === 'defensive' || value === 'aggressive'
}

function PromptPreviewBlock({ title, value }: { title: string; value: string }) {
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between gap-3">
        <div className="flex items-center gap-1.5 text-xs font-medium text-zinc-100">
          <FileText className="h-3.5 w-3.5 text-violet-200" />
          {title}
        </div>
        <span className="rounded bg-white/[0.06] px-2 py-0.5 text-[10px] text-zinc-500">
          {value.length.toLocaleString()} chars
        </span>
      </div>
      <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-lg border border-white/10 bg-black/30 p-3 text-[11px] leading-5 text-zinc-200">
        {value}
      </pre>
    </div>
  )
}

function DealerFlowPanel({
  analysis,
  language,
  onOpenGraph,
  onStartFullIndex,
  graphLoading,
  indexLoading,
}: {
  analysis: TokenAnalysis
  language: string
  onOpenGraph: () => void
  onStartFullIndex: () => void
  graphLoading: boolean
  indexLoading: boolean
}) {
  const flow = analysis.dealer_flow
  const direction = flow?.direction || 'insufficient_data'
  const tone = getDealerTone(direction)
  const signals = flow?.signal_breakdown

  return (
    <div className={`mb-4 rounded-lg border ${tone.border} ${tone.bg} p-4 2xl:p-5`}>
      <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_minmax(420px,520px)] 2xl:items-start">
        <div className="min-w-0">
          <div className={`inline-flex items-center gap-2 rounded-md border ${tone.border} bg-black/20 px-3 py-1 text-xs font-semibold ${tone.text}`}>
            <Target className="h-3.5 w-3.5" />
            {language === 'zh' ? '庄家方向判断' : 'Dealer Flow Verdict'}
          </div>
          <div className="mt-3 flex flex-wrap items-end gap-3">
            <h2 className="text-2xl font-semibold text-white sm:text-3xl">
              {getDealerDirectionLabel(direction, language)}
            </h2>
            <span className={`rounded-md border ${tone.border} bg-black/20 px-2.5 py-1 text-sm font-semibold ${tone.text}`}>
              {language === 'zh' ? '评分' : 'Score'} {typeof flow?.score === 'number' ? formatSigned(flow.score) : '0'}
            </span>
            <span className="rounded-md border border-white/10 bg-black/20 px-2.5 py-1 text-sm text-zinc-300">
              {language === 'zh' ? '信心' : 'Confidence'} {getConfidenceLabel(flow?.confidence, language)}
            </span>
            <span className="rounded-md border border-white/10 bg-black/20 px-2.5 py-1 text-sm text-zinc-300">
              {flow?.evidence_depth || analysis.depth}
            </span>
          </div>
          <p className="mt-3 max-w-3xl text-sm leading-6 text-zinc-200">
            {flow?.summary || (language === 'zh' ? '链上样本不足，暂时不能判断疑似主力方向。' : 'Not enough on-chain samples to infer suspected dealer direction.')}
          </p>
        </div>

        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 2xl:grid-cols-2">
          <SignalPill
            label={language === 'zh' ? '净流向' : 'Net flow'}
            value={formatTokenAmount(signals?.net_amount || 0)}
            positive={(signals?.net_amount || 0) >= 0}
          />
          <SignalPill label={language === 'zh' ? '买/卖次数' : 'Buy/Sell'} value={`${signals?.buy_count || 0}/${signals?.sell_count || 0}`} />
          <SignalPill label={language === 'zh' ? '净买/净卖钱包' : 'Acc/Sell wallets'} value={`${signals?.accumulator_count || 0}/${signals?.seller_count || 0}`} />
          <SignalPill label={language === 'zh' ? '关联钱包' : 'Related'} value={String(signals?.related_wallet_count || 0)} />
        </div>
      </div>

      <div className="mt-4 grid gap-4 xl:grid-cols-[1fr_1fr]">
        <div className="rounded-lg border border-white/10 bg-black/20 p-4">
          <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-white">
            <Activity className="h-4 w-4 text-[#F0B90B]" />
            {language === 'zh' ? '判断依据' : 'Evidence'}
          </div>
          <div className="space-y-2">
            {(flow?.reasons || []).slice(0, 5).map((reason) => (
              <div key={reason} className="rounded-md bg-white/[0.045] px-3 py-2 text-sm leading-5 text-zinc-200">
                {reason}
              </div>
            ))}
            {(!flow?.reasons || flow.reasons.length === 0) && (
              <div className="rounded-md bg-white/[0.045] px-3 py-2 text-sm text-zinc-500">No evidence yet</div>
            )}
          </div>
        </div>
        <div className="rounded-lg border border-white/10 bg-black/20 p-4">
          <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-white">
            <Clock3 className="h-4 w-4 text-emerald-300" />
            {language === 'zh' ? '后续监控' : 'Monitoring Plan'}
          </div>
          <div className="space-y-2">
            {(flow?.monitoring_plan || []).slice(0, 3).map((item) => (
              <div key={item} className="rounded-md bg-white/[0.045] px-3 py-2 text-sm leading-5 text-zinc-200">
                {item}
              </div>
            ))}
          </div>
          <div className="mt-4 flex flex-wrap gap-2">
            <button
              type="button"
              onClick={onOpenGraph}
              disabled={graphLoading}
              className="inline-flex items-center gap-2 rounded-md border border-emerald-300/25 bg-emerald-300/10 px-3 py-2 text-sm font-semibold text-emerald-100 disabled:opacity-60"
            >
              <Network className="h-4 w-4" />
              {graphLoading ? (language === 'zh' ? '加载关系网' : 'Loading graph') : (language === 'zh' ? '查看 3D 关系网' : 'View 3D graph')}
            </button>
            <button
              type="button"
              onClick={onStartFullIndex}
              disabled={indexLoading}
              className="inline-flex items-center gap-2 rounded-md border border-cyan-300/25 bg-cyan-300/10 px-3 py-2 text-sm font-semibold text-cyan-100 disabled:opacity-60"
            >
              <GitBranch className="h-4 w-4" />
              {language === 'zh' ? '提升为 full 数据' : 'Upgrade to full data'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function SignalPill({ label, value, positive }: { label: string; value: string; positive?: boolean }) {
  const color = typeof positive === 'boolean' ? (positive ? 'text-emerald-200' : 'text-red-200') : 'text-zinc-100'
  return (
    <div className="rounded-lg border border-white/10 bg-black/25 px-3 py-2">
      <div className="text-[11px] text-zinc-500">{label}</div>
      <div className={`mt-1 truncate font-mono text-sm font-semibold ${color}`}>{value}</div>
    </div>
  )
}

function EarlyWalletFlowPanel({
  flow,
  language,
  loading,
  onLoad,
  onStartIndex,
  onExport,
}: {
  flow?: EarlyWalletFlowResponse
  language: string
  loading: boolean
  onLoad: () => void
  onStartIndex: () => void
  onExport: () => void
}) {
  const summary = flow?.summary
  const quote = flow?.quote_symbol || 'quote'
  const direction = summary?.direction || 'insufficient_data'
  const tone = getDealerTone(direction)

  return (
    <div className={`mb-4 rounded-lg border ${tone.border} bg-black/20 p-4`}>
      <div className="flex flex-col gap-3 border-b border-white/10 pb-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <div className={`inline-flex items-center gap-2 rounded-md border ${tone.border} bg-white/[0.04] px-3 py-1 text-xs font-semibold ${tone.text}`}>
            <Layers className="h-3.5 w-3.5" />
            {language === 'zh' ? '前100早期地址资金流模型' : 'Top-100 Early Wallet Flow'}
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <h3 className="text-xl font-semibold text-white">
              {getDealerDirectionLabel(direction, language)}
            </h3>
            <span className="rounded-md border border-white/10 bg-white/[0.05] px-2.5 py-1 text-xs text-zinc-300">
              {flow?.status || (language === 'zh' ? '未计算' : 'not_loaded')}
            </span>
            <span className="rounded-md border border-white/10 bg-white/[0.05] px-2.5 py-1 text-xs text-zinc-300">
              {language === 'zh' ? '计价' : 'Quote'} {quote}
            </span>
          </div>
          <p className="mt-2 max-w-4xl text-sm leading-6 text-zinc-300">
            {flow
              ? (language === 'zh'
                ? `已追踪 ${summary?.seed_wallet_count || 0} 个初始地址、${summary?.tracked_wallet_count || 0} 个分层地址，最深 ${summary?.max_observed_depth || 0} 层。`
                : `Tracked ${summary?.seed_wallet_count || 0} seed wallets and ${summary?.tracked_wallet_count || 0} layered wallets through depth ${summary?.max_observed_depth || 0}.`)
              : (language === 'zh'
                ? '基于 full-history 索引计算前100早期买家成本、转出衍生地址和已实现盈利。'
                : 'Calculates early buyer cost basis, transfer descendants, and realized PnL from full-history indexed data.')}
          </p>
          {flow?.message && <p className="mt-2 text-sm text-amber-100">{flow.message}</p>}
          {(summary?.incomplete_reasons || []).length > 0 && (
            <div className="mt-2 flex flex-wrap gap-2">
              {summary?.incomplete_reasons?.map((reason) => (
                <span key={reason} className="rounded bg-amber-300/10 px-2 py-1 text-xs text-amber-100">{reason}</span>
              ))}
            </div>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={onLoad}
            disabled={loading}
            className="inline-flex items-center gap-2 rounded-md bg-[#F0B90B] px-3 py-2 text-sm font-semibold text-black disabled:opacity-60"
          >
            <RefreshCcw className="h-4 w-4" />
            {loading ? (language === 'zh' ? '计算中' : 'Calculating') : (language === 'zh' ? '计算模型' : 'Calculate')}
          </button>
          <button
            type="button"
            onClick={onStartIndex}
            disabled={loading}
            className="inline-flex items-center gap-2 rounded-md border border-cyan-300/25 bg-cyan-300/10 px-3 py-2 text-sm font-semibold text-cyan-100 disabled:opacity-60"
          >
            <GitBranch className="h-4 w-4" />
            {language === 'zh' ? '启动索引' : 'Start index'}
          </button>
          <button
            type="button"
            onClick={onExport}
            className="inline-flex items-center gap-2 rounded-md border border-white/10 bg-white/[0.05] px-3 py-2 text-sm font-semibold text-zinc-100"
          >
            <Download className="h-4 w-4" />
            CSV
          </button>
        </div>
      </div>

      {summary ? (
        <>
          <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <MetricCard label={language === 'zh' ? '初始总成本' : 'Initial cost'} value={formatQuote(summary.total_initial_cost, quote)} />
            <MetricCard label={language === 'zh' ? '卖出金额' : 'Sell value'} value={formatQuote(summary.total_sell_value, quote)} />
            <MetricCard label={language === 'zh' ? '已实现盈利' : 'Realized PnL'} value={formatSignedQuote(summary.realized_pnl, quote)} />
            <MetricCard label={language === 'zh' ? '剩余持仓' : 'Remaining'} value={formatTokenAmount(summary.remaining_amount)} />
            <MetricCard label={language === 'zh' ? '自身卖出' : 'Seed own sells'} value={formatQuote(summary.seed_own_sell_value, quote)} />
            <MetricCard label={language === 'zh' ? '衍生卖出' : 'Descendant sells'} value={formatQuote(summary.descendant_sell_value, quote)} />
            <MetricCard label={language === 'zh' ? '转出数量' : 'Transferred out'} value={formatTokenAmount(summary.transfer_out_amount)} />
            <MetricCard label={language === 'zh' ? '盈利率' : 'PnL %'} value={`${summary.realized_pnl_pct.toFixed(2)}%`} />
            {summary.missing_swap_price_count > 0 && (
              <MetricCard label={language === 'zh' ? '缺失单价' : 'Missing prices'} value={String(summary.missing_swap_price_count)} />
            )}
          </div>
          <div className="mt-4 grid gap-4 xl:grid-cols-2">
            <EarlySeedTable seeds={flow?.seeds || []} quote={quote} language={language} />
            <EarlyWalletTable wallets={flow?.wallets || []} quote={quote} language={language} />
          </div>
        </>
      ) : (
        <div className="mt-4 rounded-lg border border-dashed border-white/10 bg-white/[0.03] p-6 text-center text-sm text-zinc-500">
          {language === 'zh' ? '还没有计算早期地址资金流。点击“计算模型”开始。' : 'Early wallet flow has not been calculated yet. Click Calculate to start.'}
        </div>
      )}
    </div>
  )
}

function EarlySeedTable({ seeds, quote, language }: { seeds: EarlyWalletSeed[]; quote: string; language: string }) {
  return (
    <div className="rounded-lg border border-white/10 bg-white/[0.035] p-4">
      <h3 className="text-sm font-semibold text-white">{language === 'zh' ? '前100初始地址' : 'Top seed wallets'}</h3>
      <div className="mt-3 max-h-[420px] overflow-auto">
        <table className="w-full min-w-[760px] text-left text-xs">
          <thead className="sticky top-0 bg-[#10141d] text-zinc-500">
            <tr className="border-b border-white/10">
              <th className="py-2 pr-3 font-medium">#</th>
              <th className="py-2 pr-3 font-medium">Address</th>
              <th className="py-2 pr-3 font-medium">{language === 'zh' ? '买入成本' : 'Cost'}</th>
              <th className="py-2 pr-3 font-medium">{language === 'zh' ? '均价' : 'Avg'}</th>
              <th className="py-2 pr-3 font-medium">{language === 'zh' ? '自身卖出' : 'Own sell'}</th>
              <th className="py-2 pr-3 font-medium">{language === 'zh' ? '衍生盈利' : 'Desc PnL'}</th>
              <th className="py-2 pr-3 font-medium">{language === 'zh' ? '剩余' : 'Remain'}</th>
            </tr>
          </thead>
          <tbody>
            {seeds.slice(0, 100).map((seed) => (
              <tr key={seed.address} className="border-b border-white/[0.06]">
                <td className="py-2 pr-3 text-zinc-400">{seed.rank}</td>
                <td className="py-2 pr-3 font-mono text-zinc-100">{shortenAddress(seed.address)}</td>
                <td className="py-2 pr-3 font-mono text-zinc-100">{formatQuote(seed.buy_value, quote)}</td>
                <td className="py-2 pr-3 font-mono text-zinc-300">{formatSmallNumber(seed.avg_buy_price)}</td>
                <td className="py-2 pr-3 text-zinc-300">{formatQuote(seed.own_sell_value, quote)}</td>
                <td className={`py-2 pr-3 font-mono ${seed.descendant_realized_pnl >= 0 ? 'text-emerald-200' : 'text-red-200'}`}>{formatSignedQuote(seed.descendant_realized_pnl, quote)}</td>
                <td className="py-2 pr-3 text-zinc-300">{formatTokenAmount(seed.current_balance)}</td>
              </tr>
            ))}
            {seeds.length === 0 && (
              <tr><td colSpan={7} className="py-4 text-center text-zinc-500">No data</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function EarlyWalletTable({ wallets, quote, language }: { wallets: EarlyWalletFlowWallet[]; quote: string; language: string }) {
  const rows = wallets.slice(0, 120)
  return (
    <div className="rounded-lg border border-white/10 bg-white/[0.035] p-4">
      <h3 className="text-sm font-semibold text-white">{language === 'zh' ? '分层地址明细' : 'Layered wallet details'}</h3>
      <div className="mt-3 max-h-[420px] overflow-auto">
        <table className="w-full min-w-[920px] text-left text-xs">
          <thead className="sticky top-0 bg-[#10141d] text-zinc-500">
            <tr className="border-b border-white/10">
              <th className="py-2 pr-3 font-medium">Depth</th>
              <th className="py-2 pr-3 font-medium">Address</th>
              <th className="py-2 pr-3 font-medium">Root</th>
              <th className="py-2 pr-3 font-medium">Buy/Sell</th>
              <th className="py-2 pr-3 font-medium">{language === 'zh' ? '转入/转出' : 'In/Out'}</th>
              <th className="py-2 pr-3 font-medium">{language === 'zh' ? '卖出金额' : 'Sell value'}</th>
              <th className="py-2 pr-3 font-medium">PnL</th>
              <th className="py-2 pr-3 font-medium">{language === 'zh' ? '持仓' : 'Balance'}</th>
              <th className="py-2 pr-3 font-medium">{language === 'zh' ? '缺价' : 'No price'}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((wallet) => (
              <tr key={`${wallet.depth}-${wallet.address}-${wallet.root_address}`} className="border-b border-white/[0.06]">
                <td className="py-2 pr-3 text-zinc-400">{wallet.depth}</td>
                <td className="py-2 pr-3 font-mono text-zinc-100">{shortenAddress(wallet.address)}</td>
                <td className="py-2 pr-3 font-mono text-zinc-400">{wallet.root_address ? shortenAddress(wallet.root_address) : '-'}</td>
                <td className="py-2 pr-3 text-zinc-300">{formatTokenAmount(wallet.buy_amount)}/{formatTokenAmount(wallet.sell_amount)}</td>
                <td className="py-2 pr-3 text-zinc-300">{formatTokenAmount(wallet.transfer_in_amount)}/{formatTokenAmount(wallet.transfer_out_amount)}</td>
                <td className="py-2 pr-3 text-zinc-300">{formatQuote(wallet.sell_value, quote)}</td>
                <td className={`py-2 pr-3 font-mono ${wallet.realized_pnl >= 0 ? 'text-emerald-200' : 'text-red-200'}`}>{formatSignedQuote(wallet.realized_pnl, quote)}</td>
                <td className="py-2 pr-3 text-zinc-300">{formatTokenAmount(wallet.current_balance)}</td>
                <td className="py-2 pr-3 text-zinc-400">{wallet.incomplete_pricing || '-'}</td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr><td colSpan={9} className="py-4 text-center text-zinc-500">No data</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function DealerCharts({ analysis, language }: { analysis: TokenAnalysis; language: string }) {
  const flow = analysis.dealer_flow
  const buckets = (analysis.full?.first_buy_buckets || analysis.recent?.first_buy_buckets || []).slice(-10)
  const wallets = [
    ...(analysis.full?.top_accumulators || analysis.recent?.top_accumulators || []).slice(0, 5),
    ...(analysis.full?.top_sellers || analysis.recent?.top_sellers || []).slice(0, 5),
  ].sort((a, b) => Math.abs(b.net_bought_amount) - Math.abs(a.net_bought_amount)).slice(0, 8)

  const flowData = [
    { name: language === 'zh' ? '净买入' : 'Accumulated', value: flow?.signal_breakdown.accumulator_net_amount || 0 },
    { name: language === 'zh' ? '净卖出' : 'Distributed', value: flow?.signal_breakdown.seller_net_amount || 0 },
    { name: language === 'zh' ? '合计净流向' : 'Net', value: flow?.signal_breakdown.net_amount || 0 },
  ]
  const bucketData = buckets.map((bucket) => ({
    name: bucket.bucket.slice(5),
    buyers: bucket.buyer_count,
    net: bucket.net_amount,
  }))
  const walletData = wallets.map((wallet) => ({
    name: shortenAddress(wallet.address),
    net: wallet.net_bought_amount,
  }))

  return (
    <div className="grid gap-4 xl:grid-cols-3">
      <ChartPanel title={language === 'zh' ? '资金流向' : 'Flow'}>
        <ResponsiveContainer width="100%" height={220}>
          <BarChart data={flowData}>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" vertical={false} />
            <XAxis dataKey="name" tick={{ fill: '#a1a1aa', fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fill: '#71717a', fontSize: 11 }} axisLine={false} tickLine={false} tickFormatter={(value) => formatTokenAmount(Number(value))} />
            <Tooltip content={<ChartTooltip />} cursor={{ fill: 'rgba(255,255,255,0.04)' }} />
            <Bar dataKey="value" radius={[4, 4, 0, 0]}>
              {flowData.map((entry) => (
                <Cell key={entry.name} fill={entry.value >= 0 ? '#34d399' : '#f87171'} />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </ChartPanel>
      <ChartPanel title={language === 'zh' ? '首次买入分布' : 'First Buy Buckets'}>
        <ResponsiveContainer width="100%" height={220}>
          <BarChart data={bucketData}>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" vertical={false} />
            <XAxis dataKey="name" tick={{ fill: '#a1a1aa', fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fill: '#71717a', fontSize: 11 }} axisLine={false} tickLine={false} />
            <Tooltip content={<ChartTooltip />} cursor={{ fill: 'rgba(255,255,255,0.04)' }} />
            <Bar dataKey="buyers" fill="#F0B90B" radius={[4, 4, 0, 0]} />
          </BarChart>
        </ResponsiveContainer>
      </ChartPanel>
      <ChartPanel title={language === 'zh' ? 'Top 钱包净流向' : 'Top Wallet Net Flow'}>
        <ResponsiveContainer width="100%" height={220}>
          <BarChart data={walletData} layout="vertical" margin={{ left: 4, right: 8 }}>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" horizontal={false} />
            <XAxis type="number" tick={{ fill: '#71717a', fontSize: 11 }} axisLine={false} tickLine={false} tickFormatter={(value) => formatTokenAmount(Number(value))} />
            <YAxis type="category" dataKey="name" width={72} tick={{ fill: '#a1a1aa', fontSize: 11 }} axisLine={false} tickLine={false} />
            <Tooltip content={<ChartTooltip />} cursor={{ fill: 'rgba(255,255,255,0.04)' }} />
            <Bar dataKey="net" radius={[0, 4, 4, 0]}>
              {walletData.map((entry) => (
                <Cell key={entry.name} fill={entry.net >= 0 ? '#34d399' : '#f87171'} />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </ChartPanel>
    </div>
  )
}

function ChartPanel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-white/10 bg-white/[0.035] p-4">
      <h3 className="mb-3 text-sm font-semibold text-white">{title}</h3>
      {children}
    </div>
  )
}

function ChartTooltip({ active, payload, label }: { active?: boolean; payload?: Array<{ name: string; value: number }>; label?: string }) {
  if (!active || !payload || payload.length === 0) return null
  return (
    <div className="rounded-md border border-white/10 bg-[#0b0f17] px-3 py-2 text-xs text-zinc-100 shadow-xl">
      <div className="mb-1 font-medium">{label}</div>
      {payload.map((item) => (
        <div key={item.name} className="flex justify-between gap-4">
          <span className="text-zinc-400">{item.name}</span>
          <span className="font-mono">{formatTokenAmount(Number(item.value))}</span>
        </div>
      ))}
    </div>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md bg-white/[0.04] px-3 py-2">
      <div className="text-xs text-zinc-500">{label}</div>
      <div className="mt-1 truncate font-mono text-sm text-zinc-100">{value}</div>
    </div>
  )
}

function MetricCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-white/10 bg-black/20 p-3">
      <div className="text-xs uppercase text-zinc-500">{label}</div>
      <div className="mt-2 text-lg font-semibold text-zinc-100">{value}</div>
    </div>
  )
}

function MiniList({ title, items }: { title: string; items: Array<{ name: string; value: string }> }) {
  return (
    <div className="rounded-lg border border-white/10 bg-white/[0.035] p-4">
      <h3 className="text-sm font-semibold text-white">{title}</h3>
      <div className="mt-3 space-y-2">
        {items.length > 0 ? items.map((item) => (
          <div key={`${title}-${item.name}`} className="flex items-center justify-between gap-3 rounded-md bg-white/[0.04] px-3 py-2 text-sm">
            <span className="truncate text-zinc-100">{item.name}</span>
            <span className="shrink-0 text-xs text-zinc-400">{item.value}</span>
          </div>
        )) : (
          <div className="rounded-md bg-white/[0.04] px-3 py-2 text-sm text-zinc-500">No data</div>
        )}
      </div>
    </div>
  )
}

function WalletTable({ title, wallets }: { title: string; wallets: WalletAnalysis[] }) {
  return (
    <div className="rounded-lg border border-white/10 bg-white/[0.035] p-4">
      <h3 className="text-sm font-semibold text-white">{title}</h3>
      <div className="mt-3 overflow-x-auto">
        <table className="w-full min-w-[620px] text-left text-xs">
          <thead className="text-zinc-500">
            <tr className="border-b border-white/10">
              <th className="py-2 pr-3 font-medium">Address</th>
              <th className="py-2 pr-3 font-medium">Type</th>
              <th className="py-2 pr-3 font-medium">First buy</th>
              <th className="py-2 pr-3 font-medium">Buy/Sell</th>
              <th className="py-2 pr-3 font-medium">Net</th>
            </tr>
          </thead>
          <tbody>
            {wallets.slice(0, 10).map((wallet) => (
              <tr key={`${title}-${wallet.address}`} className="border-b border-white/[0.06]">
                <td className="py-2 pr-3 font-mono text-zinc-100">{shortenAddress(wallet.address)}</td>
                <td className="py-2 pr-3 text-zinc-300">{wallet.wallet_type}</td>
                <td className="py-2 pr-3 text-zinc-400">{wallet.first_buy_at ? wallet.first_buy_at.replace('T', ' ').replace('Z', '') : '-'}</td>
                <td className="py-2 pr-3 text-zinc-300">{wallet.buy_count}/{wallet.sell_count}</td>
                <td className="py-2 pr-3 font-mono text-zinc-100">{formatTokenAmount(wallet.net_bought_amount)}</td>
              </tr>
            ))}
            {wallets.length === 0 && (
              <tr>
                <td colSpan={5} className="py-4 text-center text-zinc-500">No data</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function AIReportWindow({
  token,
  language,
  agentConfig,
  aiModels,
  onAgentConfigChange,
  onClose,
  onGenerate,
  onPreviewPrompt,
}: {
  token: ManagedAnalysisToken
  language: string
  agentConfig: OnchainReportAgentConfig
  aiModels: AIModel[]
  onAgentConfigChange: (config: OnchainReportAgentConfig) => void
  onClose: () => void
  onGenerate: () => void
  onPreviewPrompt: () => void
}) {
  const title = token.analysis?.token?.symbol || token.label || shortenAddress(token.address)
  const [activeTab, setActiveTab] = useState<'report' | 'prompt' | 'agent'>('report')
  const selectedModelName = getSelectedReportModelName(agentConfig.modelID, aiModels, language)
  const updateAgentConfig = (patch: Partial<OnchainReportAgentConfig>) => onAgentConfigChange({ ...agentConfig, ...patch })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4 py-6">
      <section className="flex max-h-[88vh] w-full max-w-4xl flex-col rounded-lg border border-white/10 bg-[#0b0f17] shadow-2xl shadow-black">
        <header className="flex items-start justify-between gap-4 border-b border-white/10 px-5 py-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-semibold text-violet-200">
              <Brain className="h-4 w-4" />
              {language === 'zh' ? 'AI 链上分析报告' : 'AI On-chain Report'}
            </div>
            <h2 className="mt-1 truncate text-xl font-semibold text-white">{title}</h2>
            <div className="mt-1 break-all font-mono text-xs text-zinc-500">{token.address}</div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-white/10 bg-white/[0.04] text-zinc-300 hover:bg-white/[0.08]"
            aria-label={language === 'zh' ? '关闭报告' : 'Close report'}
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="grid grid-cols-3 border-b border-white/10 text-sm">
          <button
            type="button"
            onClick={() => setActiveTab('report')}
            className={`inline-flex items-center justify-center gap-2 px-3 py-3 font-medium ${activeTab === 'report' ? 'border-b-2 border-[#F0B90B] text-[#F0B90B]' : 'text-zinc-400 hover:text-zinc-100'}`}
          >
            <FileText className="h-4 w-4" />
            {language === 'zh' ? '报告' : 'Report'}
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('prompt')}
            className={`inline-flex items-center justify-center gap-2 px-3 py-3 font-medium ${activeTab === 'prompt' ? 'border-b-2 border-violet-300 text-violet-200' : 'text-zinc-400 hover:text-zinc-100'}`}
          >
            <Eye className="h-4 w-4" />
            {language === 'zh' ? '提示词预览' : 'Prompt'}
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('agent')}
            className={`inline-flex items-center justify-center gap-2 px-3 py-3 font-medium ${activeTab === 'agent' ? 'border-b-2 border-cyan-300 text-cyan-200' : 'text-zinc-400 hover:text-zinc-100'}`}
          >
            <Settings className="h-4 w-4" />
            {language === 'zh' ? '分析 Agent' : 'Agent'}
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {activeTab === 'report' && (
            token.aiReportLoading ? (
              <div className="rounded-lg border border-violet-300/20 bg-violet-300/[0.08] p-6 text-sm text-violet-100">
                {language === 'zh' ? 'AI 正在根据该币种的链上数据生成报告...' : 'AI is writing a report from this token on-chain data...'}
              </div>
            ) : token.aiReport ? (
              <>
                <div className="mb-4 flex flex-wrap gap-2 text-xs text-zinc-400">
                  <span className="rounded bg-white/[0.05] px-2 py-1">
                    {language === 'zh' ? '模型' : 'Model'}: {token.aiReport.model_name || token.aiReport.model_id || '-'}
                  </span>
                  <span className="rounded bg-white/[0.05] px-2 py-1">
                    {language === 'zh' ? '生成时间' : 'Generated'}: {formatNullableTime(token.aiReport.generated_at)}
                  </span>
                  <span className="rounded bg-white/[0.05] px-2 py-1">
                    {language === 'zh' ? '当前 Agent 模型' : 'Current agent model'}: {selectedModelName}
                  </span>
                </div>
                <article className="whitespace-pre-wrap rounded-lg border border-white/10 bg-black/25 p-5 text-sm leading-7 text-zinc-100">
                  {token.aiReport.report}
                </article>
              </>
            ) : (
              <div className="rounded-lg border border-dashed border-violet-300/25 bg-violet-300/[0.06] p-6 text-sm leading-6 text-zinc-300">
                <div className="font-semibold text-white">
                  {language === 'zh' ? '这个币种还没有 AI 报告' : 'No AI report for this token yet'}
                </div>
                <p className="mt-2">
                  {token.aiReportError || (language === 'zh'
                    ? '先在「分析 Agent」里选择模型和报告策略，然后点击下方按钮生成。'
                    : 'Choose model and report strategy in Agent, then generate the report.')}
                </p>
              </div>
            )
          )}

          {activeTab === 'prompt' && (
            <div className="space-y-4">
              <div className="flex flex-col gap-3 rounded-lg border border-white/10 bg-black/20 p-4 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div className="text-sm font-semibold text-white">
                    {language === 'zh' ? '生成报告前先看 AI 输入' : 'Preview AI input before generation'}
                  </div>
                  <div className="mt-1 text-xs text-zinc-500">
                    {language === 'zh' ? '这和策略页的提示词预览类似，只预览，不调用模型。' : 'Similar to strategy prompt preview. It previews only and does not call the model.'}
                  </div>
                </div>
                <button
                  type="button"
                  onClick={onPreviewPrompt}
                  disabled={token.aiReportPromptLoading}
                  className="inline-flex items-center justify-center gap-2 rounded-md border border-violet-300/25 bg-violet-300/10 px-4 py-2 text-sm font-semibold text-violet-100 disabled:opacity-60"
                >
                  <Eye className="h-4 w-4" />
                  {token.aiReportPromptLoading ? (language === 'zh' ? '生成中' : 'Loading') : (language === 'zh' ? '生成提示词预览' : 'Preview prompt')}
                </button>
              </div>

              {token.aiReportPromptError && (
                <div className="rounded-md border border-red-300/25 bg-red-400/10 px-3 py-2 text-sm text-red-100">
                  {token.aiReportPromptError}
                </div>
              )}

              {token.aiReportPrompt ? (
                <>
                  <PromptPreviewBlock title="System Prompt" value={token.aiReportPrompt.system_prompt} />
                  <PromptPreviewBlock title="User Prompt" value={token.aiReportPrompt.user_prompt} />
                </>
              ) : (
                <div className="rounded-lg border border-dashed border-white/10 bg-white/[0.03] p-6 text-center text-sm text-zinc-500">
                  {language === 'zh' ? '还没有生成提示词预览' : 'No prompt preview yet'}
                </div>
              )}
            </div>
          )}

          {activeTab === 'agent' && (
            <div className="space-y-4">
              <div className="rounded-lg border border-cyan-300/20 bg-cyan-300/[0.06] p-4">
                <div className="mb-4 flex items-center gap-2 text-sm font-semibold text-cyan-100">
                  <Bot className="h-4 w-4" />
                  {language === 'zh' ? '链上分析报告 Agent 设置' : 'On-chain report agent settings'}
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <label className="block">
                    <span className="mb-1 block text-xs text-zinc-400">{language === 'zh' ? 'AI 模型' : 'AI model'}</span>
                    <select
                      value={agentConfig.modelID}
                      onChange={(event) => updateAgentConfig({ modelID: event.target.value })}
                      className="w-full rounded-md border border-white/10 bg-black/30 px-3 py-2 text-sm text-zinc-100 outline-none focus:border-cyan-300/60"
                    >
                      <option value="">{language === 'zh' ? '自动选择可用模型' : 'Auto select enabled model'}</option>
                      {aiModels.map((model) => (
                        <option key={model.id} value={model.id}>
                          {model.name} ({model.provider}{model.customModelName ? ` · ${model.customModelName}` : ''})
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="block">
                    <span className="mb-1 block text-xs text-zinc-400">{language === 'zh' ? '报告风格' : 'Report style'}</span>
                    <select
                      value={agentConfig.reportStyle}
                      onChange={(event) => updateAgentConfig({ reportStyle: event.target.value as OnchainReportStyle })}
                      className="w-full rounded-md border border-white/10 bg-black/30 px-3 py-2 text-sm text-zinc-100 outline-none focus:border-cyan-300/60"
                    >
                      <option value="balanced">{language === 'zh' ? '均衡报告' : 'Balanced'}</option>
                      <option value="brief">{language === 'zh' ? '简短摘要' : 'Brief'}</option>
                      <option value="deep">{language === 'zh' ? '深度分析' : 'Deep'}</option>
                      <option value="watchlist">{language === 'zh' ? '监控清单' : 'Watchlist'}</option>
                    </select>
                  </label>
                  <label className="block">
                    <span className="mb-1 block text-xs text-zinc-400">{language === 'zh' ? '风险偏好' : 'Risk profile'}</span>
                    <select
                      value={agentConfig.riskProfile}
                      onChange={(event) => updateAgentConfig({ riskProfile: event.target.value as OnchainReportRiskProfile })}
                      className="w-full rounded-md border border-white/10 bg-black/30 px-3 py-2 text-sm text-zinc-100 outline-none focus:border-cyan-300/60"
                    >
                      <option value="balanced">{language === 'zh' ? '均衡' : 'Balanced'}</option>
                      <option value="defensive">{language === 'zh' ? '防守谨慎' : 'Defensive'}</option>
                      <option value="aggressive">{language === 'zh' ? '进攻观察' : 'Aggressive'}</option>
                    </select>
                  </label>
                  <label className="flex items-center justify-between gap-3 rounded-md border border-white/10 bg-black/20 px-3 py-2">
                    <span className="text-sm text-zinc-100">{language === 'zh' ? '包含原始信号附录' : 'Include raw signal appendix'}</span>
                    <input
                      type="checkbox"
                      checked={agentConfig.includeRawSignals}
                      onChange={(event) => updateAgentConfig({ includeRawSignals: event.target.checked })}
                      className="h-4 w-4 accent-[#F0B90B]"
                    />
                  </label>
                </div>
                <label className="mt-4 block">
                  <span className="mb-1 block text-xs text-zinc-400">{language === 'zh' ? '额外关注点' : 'Extra focus'}</span>
                  <input
                    value={agentConfig.focus}
                    onChange={(event) => updateAgentConfig({ focus: event.target.value })}
                    className="w-full rounded-md border border-white/10 bg-black/30 px-3 py-2 text-sm text-zinc-100 outline-none focus:border-cyan-300/60"
                    placeholder={language === 'zh' ? '例如：重点看巨鲸净买入、LP 风险、卖压来源' : 'e.g. whale accumulation, LP risk, sell pressure source'}
                  />
                </label>
                <label className="mt-4 block">
                  <span className="mb-1 block text-xs text-zinc-400">{language === 'zh' ? '自定义提示词' : 'Custom prompt'}</span>
                  <textarea
                    value={agentConfig.customPrompt}
                    onChange={(event) => updateAgentConfig({ customPrompt: event.target.value })}
                    className="h-28 w-full resize-none rounded-md border border-white/10 bg-black/30 px-3 py-2 text-sm leading-6 text-zinc-100 outline-none focus:border-cyan-300/60"
                    placeholder={language === 'zh' ? '补充这个报告 Agent 的分析要求。不要写绝对买卖指令。' : 'Add report agent instructions. Avoid absolute buy/sell instructions.'}
                  />
                </label>
              </div>
            </div>
          )}
        </div>

        <footer className="flex flex-col gap-2 border-t border-white/10 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-xs text-zinc-500">
            {language === 'zh' ? '报告只做交易辅助分析，不会触发下单。' : 'Reports are decision support only and do not place trades.'}
          </p>
          <button
            type="button"
            onClick={onGenerate}
            disabled={token.aiReportLoading}
            className="inline-flex items-center justify-center gap-2 rounded-md bg-[#F0B90B] px-4 py-2 text-sm font-semibold text-black disabled:opacity-60"
          >
            <Brain className="h-4 w-4" />
            {token.aiReportLoading ? (language === 'zh' ? '生成中' : 'Writing') : (language === 'zh' ? '重新生成报告' : 'Regenerate report')}
          </button>
        </footer>
      </section>
    </div>
  )
}

function WalletGraphWindow({
  token,
  language,
  onClose,
  onRefresh,
}: {
  token: ManagedAnalysisToken
  language: string
  onClose: () => void
  onRefresh: () => void
}) {
  const graph = token.walletGraph
  const earlyFlow = token.earlyWalletFlow || token.analysis?.early_wallet_flow
  const title = token.analysis?.token?.symbol || token.label || shortenAddress(token.address)
  const graphData = useMemo(() => buildDisplayedGraphData(graph, earlyFlow), [graph, earlyFlow])

  const nodeColor = (node: ForceGraphNode) => {
    switch (node.node_type) {
      case 'token':
        return '#F0B90B'
      case 'pool':
        return '#38bdf8'
      case 'accumulator':
        return '#34d399'
      case 'seller':
        return '#f87171'
      case 'related_wallet':
        return '#a78bfa'
      case 'top_holder':
        return '#fbbf24'
      case 'early_seed':
        return '#fb923c'
      case 'early_descendant':
        return '#c084fc'
      case 'owner':
      case 'creator':
        return '#fb7185'
      default:
        return '#d4d4d8'
    }
  }
  const linkColor = (link: ForceGraphLink) => {
    switch (link.relation) {
      case 'buy':
      case 'related':
        return 'rgba(52, 211, 153, 0.72)'
      case 'sell':
        return 'rgba(248, 113, 113, 0.72)'
      case 'transfer':
        return 'rgba(167, 139, 250, 0.7)'
      case 'pool':
        return 'rgba(56, 189, 248, 0.58)'
      default:
        return 'rgba(240, 185, 11, 0.55)'
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 px-3 py-4">
      <section className="flex h-[92vh] w-full max-w-6xl flex-col rounded-lg border border-white/10 bg-[#070a11] shadow-2xl shadow-black">
        <header className="flex items-start justify-between gap-4 border-b border-white/10 px-5 py-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-semibold text-emerald-200">
              <Network className="h-4 w-4" />
              {language === 'zh' ? '3D 钱包关系网' : '3D Wallet Graph'}
            </div>
            <h2 className="mt-1 truncate text-xl font-semibold text-white">{title}</h2>
            <div className="mt-1 break-all font-mono text-xs text-zinc-500">{token.address}</div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <button
              type="button"
              onClick={onRefresh}
              disabled={token.walletGraphLoading}
              className="inline-flex items-center gap-2 rounded-md border border-emerald-300/25 bg-emerald-300/10 px-3 py-2 text-sm font-semibold text-emerald-100 disabled:opacity-60"
            >
              <RefreshCcw className="h-4 w-4" />
              {token.walletGraphLoading ? (language === 'zh' ? '刷新中' : 'Refreshing') : (language === 'zh' ? '刷新' : 'Refresh')}
            </button>
            <button
              type="button"
              onClick={onClose}
              className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-white/10 bg-white/[0.04] text-zinc-300 hover:bg-white/[0.08]"
              aria-label={language === 'zh' ? '关闭关系网' : 'Close graph'}
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </header>

        <div className="grid min-h-0 flex-1 gap-0 lg:grid-cols-[1fr_300px]">
          <div className="relative min-h-[520px] overflow-hidden bg-black">
            {token.walletGraphLoading && (
              <div className="absolute inset-0 z-10 flex items-center justify-center bg-black/50 text-sm text-emerald-100">
                {language === 'zh' ? '正在生成关系网...' : 'Loading wallet graph...'}
              </div>
            )}
            {token.walletGraphError ? (
              <div className="flex h-full items-center justify-center p-8 text-center text-sm text-red-100">
                {token.walletGraphError}
              </div>
            ) : graphData.nodes.length > 0 ? (
              <Suspense fallback={<div className="flex h-full items-center justify-center text-sm text-emerald-100">{language === 'zh' ? '正在加载 3D 引擎...' : 'Loading 3D engine...'}</div>}>
                <ForceGraph3D
                  graphData={graphData}
                  backgroundColor="#000000"
                  showNavInfo={false}
                  nodeLabel={(node) => graphNodeLabel(node as ForceGraphNode, language)}
                  linkLabel={(link) => graphLinkLabel(link as ForceGraphLink, language)}
                  nodeColor={(node) => nodeColor(node as ForceGraphNode)}
                  nodeVal={(node) => Math.max(4, Math.min(28, Math.log10(Math.abs((node as ForceGraphNode).value || 1) + 10) * 5))}
                  linkColor={(link) => linkColor(link as ForceGraphLink)}
                  linkWidth={(link) => Math.max(0.6, Math.min(4, Math.log10(Math.abs((link as ForceGraphLink).weight || 1) + 1)))}
                  linkDirectionalArrowLength={3.5}
                  linkDirectionalArrowRelPos={1}
                  linkDirectionalParticles={(link) => (link as ForceGraphLink).relation === 'transfer' ? 2 : 1}
                  linkDirectionalParticleSpeed={0.004}
                />
              </Suspense>
            ) : (
              <div className="flex h-full items-center justify-center p-8 text-center text-sm text-zinc-500">
                {language === 'zh' ? '还没有关系网数据。点击刷新重试。' : 'No graph data yet. Refresh to retry.'}
              </div>
            )}
          </div>

          <aside className="min-h-0 overflow-y-auto border-t border-white/10 bg-white/[0.03] p-4 lg:border-l lg:border-t-0">
            <div className="rounded-lg border border-white/10 bg-black/25 p-4">
              <div className="text-sm font-semibold text-white">
                {language === 'zh' ? '当前结论' : 'Current verdict'}
              </div>
              <div className={`mt-2 text-lg font-semibold ${getDealerToneClass(graph?.dealer_flow?.direction)}`}>
                {getDealerDirectionLabel(graph?.dealer_flow?.direction, language)}
              </div>
              <div className="mt-2 text-xs leading-5 text-zinc-400">
                {graph?.dealer_flow?.summary || (language === 'zh' ? '暂无庄家方向判断。' : 'No dealer flow verdict yet.')}
              </div>
            </div>

            <div className="mt-4 grid grid-cols-2 gap-2">
              <Metric label={language === 'zh' ? '节点' : 'Nodes'} value={String(graphData.nodes.length)} />
              <Metric label={language === 'zh' ? '关系' : 'Edges'} value={String(graphData.links.length)} />
              <Metric label={language === 'zh' ? '深度' : 'Depth'} value={String(earlyFlow?.summary?.max_observed_depth ?? graph?.depth ?? token.depth)} />
              <Metric label={language === 'zh' ? '状态' : 'Status'} value={earlyFlow?.status || graph?.status || '-'} />
            </div>

            <div className="mt-4 space-y-2">
              <GraphLegend color="#F0B90B" label={language === 'zh' ? '代币' : 'Token'} />
              <GraphLegend color="#38bdf8" label={language === 'zh' ? '池子' : 'Pool'} />
              <GraphLegend color="#34d399" label={language === 'zh' ? '疑似进货钱包' : 'Accumulator'} />
              <GraphLegend color="#f87171" label={language === 'zh' ? '疑似出货钱包' : 'Seller'} />
              <GraphLegend color="#a78bfa" label={language === 'zh' ? '关联/套利钱包' : 'Related'} />
              <GraphLegend color="#fb923c" label={language === 'zh' ? '早期初始地址' : 'Early seed'} />
              <GraphLegend color="#c084fc" label={language === 'zh' ? '衍生地址' : 'Descendant'} />
              <GraphLegend color="#fbbf24" label="Top holder" />
              <GraphLegend color="#fb7185" label="Owner / Creator" />
            </div>
          </aside>
        </div>
      </section>
    </div>
  )
}

function GraphLegend({ color, label }: { color: string; label: string }) {
  return (
    <div className="flex items-center gap-2 rounded-md bg-white/[0.04] px-3 py-2 text-xs text-zinc-300">
      <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: color }} />
      {label}
    </div>
  )
}

function buildDisplayedGraphData(graph: WalletGraphResponse | undefined, earlyFlow: EarlyWalletFlowResponse | undefined) {
  if (earlyFlow?.wallets && earlyFlow.wallets.length > 0) {
    const nodes: ForceGraphNode[] = [
      {
        id: `token:${earlyFlow.address}`,
        label: earlyFlow.token?.symbol || shortenAddress(earlyFlow.address),
        node_type: 'token',
        address: earlyFlow.address,
        value: Math.max(earlyFlow.summary.total_initial_cost || 1, 1),
      },
    ]
    for (const wallet of earlyFlow.wallets.slice(0, 160)) {
      nodes.push({
        id: `early:${wallet.address}`,
        label: shortenAddress(wallet.address),
        node_type: wallet.is_seed ? 'early_seed' : 'early_descendant',
        address: wallet.address,
        wallet_type: wallet.is_seed ? 'seed' : `depth_${wallet.depth}`,
        value: Math.max(Math.abs(wallet.allocated_cost) + Math.abs(wallet.realized_pnl) + Math.abs(wallet.current_balance), 1),
        buy_count: wallet.buy_count,
        sell_count: wallet.sell_count,
        buy_amount: wallet.buy_amount,
        sell_amount: wallet.sell_amount,
        net_bought_amount: wallet.current_balance,
        first_buy_at: wallet.first_seen_at,
      })
    }
    const nodeIDs = new Set(nodes.map((node) => node.id))
    const links: ForceGraphLink[] = []
    for (const seed of earlyFlow.seeds || []) {
      const target = `early:${seed.address}`
      if (nodeIDs.has(target)) {
        links.push({
          source: `token:${earlyFlow.address}`,
          target,
          relation: 'early_seed',
          amount: seed.buy_amount,
          weight: Math.max(Math.log10(Math.abs(seed.buy_value) + 10), 1),
        })
      }
    }
    for (const edge of earlyFlow.edges || []) {
      const source = `early:${edge.source}`
      const target = `early:${edge.target}`
      if (!nodeIDs.has(source) || !nodeIDs.has(target)) continue
      links.push({
        source,
        target,
        relation: 'transfer',
        amount: edge.amount,
        weight: Math.max(Math.log10(Math.abs(edge.amount) + 10), 1),
        tx_hash: edge.tx_hash,
      })
    }
    return { nodes, links }
  }
  return {
    nodes: (graph?.nodes || []) as ForceGraphNode[],
    links: (graph?.edges || []).map((edge) => ({
      ...edge,
      source: edge.source,
      target: edge.target,
    })) as ForceGraphLink[],
  }
}

function graphNodeLabel(node: ForceGraphNode, language: string): string {
  const parts = [
    `<div><b>${node.label || shortenAddress(node.address || node.id)}</b></div>`,
    `<div>${language === 'zh' ? '类型' : 'Type'}: ${node.node_type}</div>`,
  ]
  if (node.address) parts.push(`<div>${shortenAddress(node.address)}</div>`)
  if (typeof node.net_bought_amount === 'number') parts.push(`<div>Net: ${formatTokenAmount(node.net_bought_amount)}</div>`)
  if (typeof node.buy_count === 'number' || typeof node.sell_count === 'number') parts.push(`<div>Buy/Sell: ${node.buy_count || 0}/${node.sell_count || 0}</div>`)
  if (node.first_buy_at) parts.push(`<div>${language === 'zh' ? '首次买入' : 'First buy'}: ${node.first_buy_at.replace('T', ' ').replace('Z', '')}</div>`)
  return parts.join('')
}

function graphLinkLabel(link: ForceGraphLink, language: string): string {
  const parts = [
    `<div><b>${language === 'zh' ? '关系' : 'Relation'}: ${link.relation}</b></div>`,
  ]
  if (typeof link.amount === 'number') parts.push(`<div>Amount: ${formatTokenAmount(link.amount)}</div>`)
  if (link.tx_hash) parts.push(`<div>${shortenAddress(link.tx_hash)}</div>`)
  return parts.join('')
}

function shortenAddress(address: string): string {
  if (!address || address.length < 12) return address || '-'
  return `${address.slice(0, 6)}...${address.slice(-4)}`
}

function normalizeEVMAddress(address: string): string {
  return address.trim().toLowerCase()
}

function mergeAnalysisWithWalletGraph(
  analysis: TokenAnalysis | undefined,
  graph: WalletGraphResponse | undefined,
): TokenAnalysis | undefined {
  if (!analysis || !graph?.dealer_flow) return analysis
  return {
    ...analysis,
    dealer_flow: graph.dealer_flow,
    token: analysis.token || graph.token,
    status: analysis.status || graph.status,
  }
}

function mergeAnalysisWithEarlyWalletFlow(
  analysis: TokenAnalysis | undefined,
  flow: EarlyWalletFlowResponse | undefined,
): TokenAnalysis | undefined {
  if (!analysis || !flow) return analysis
  return {
    ...analysis,
    early_wallet_flow: flow,
    token: analysis.token || flow.token,
  }
}

function clampMonitorIntervalMinutes(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_MONITOR_INTERVAL_MINUTES
  return Math.min(MAX_MONITOR_INTERVAL_MINUTES, Math.max(MIN_MONITOR_INTERVAL_MINUTES, Math.round(value)))
}

function getMonitorIntervalMS(token: Pick<ManagedAnalysisToken, 'monitorIntervalMinutes'>): number {
  return clampMonitorIntervalMinutes(token.monitorIntervalMinutes) * 60 * 1000
}

function loadManagedTokens(): ManagedAnalysisToken[] {
  if (typeof window === 'undefined') {
    return defaultManagedTokens()
  }
  try {
    const raw = localStorage.getItem(ONCHAIN_MANAGER_STORAGE_KEY)
    if (!raw) return defaultManagedTokens()
    const parsed = JSON.parse(raw) as ManagedAnalysisToken[]
    if (!Array.isArray(parsed)) return defaultManagedTokens()
    const tokens = parsed
      .filter((token) => token && token.chain === 'bsc' && evmAddressPattern.test(token.address))
      .map((token) => ({
        ...token,
        id: token.id || `bsc:${normalizeEVMAddress(token.address)}`,
        address: normalizeEVMAddress(token.address),
        depth: token.depth === 'full' ? 'full' as const : 'recent' as const,
        monitorEnabled: token.monitorEnabled ?? true,
        monitorIntervalMinutes: clampMonitorIntervalMinutes(token.monitorIntervalMinutes || DEFAULT_MONITOR_INTERVAL_MINUTES),
        isLoading: false,
        error: '',
      }))
    return tokens.length > 0 ? tokens : defaultManagedTokens()
  } catch {
    return defaultManagedTokens()
  }
}

function defaultManagedTokens(): ManagedAnalysisToken[] {
  return [{
    id: `bsc:${DEFAULT_ONCHAIN_ADDRESS}`,
    chain: 'bsc',
    address: DEFAULT_ONCHAIN_ADDRESS,
    depth: 'recent',
    label: 'Demo token',
    monitorEnabled: true,
    monitorIntervalMinutes: DEFAULT_MONITOR_INTERVAL_MINUTES,
  }]
}

function formatShortTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function formatNullableTime(value?: string): string {
  if (!value) return '-'
  return formatShortTime(value)
}

function formatNextMonitorTime(token: ManagedAnalysisToken): string {
  if (!token.monitorEnabled) return '-'
  const base = token.lastCheckedAt || token.updatedAt
  if (!base) return 'soon'
  const checkedAt = new Date(base).getTime()
  if (Number.isNaN(checkedAt)) return 'soon'
  return formatShortTime(new Date(checkedAt + getMonitorIntervalMS(token)).toISOString())
}

function formatMonitorInterval(minutes: number): string {
  const safeMinutes = clampMonitorIntervalMinutes(minutes)
  if (safeMinutes < 60) return `${safeMinutes}m`
  if (safeMinutes % 60 === 0) return `${safeMinutes / 60}h`
  return `${safeMinutes}m`
}

function getDealerDirectionLabel(direction: DealerDirection | undefined, language: string): string {
  switch (direction) {
    case 'accumulating':
      return language === 'zh' ? '疑似进货' : 'Accumulating'
    case 'distributing':
      return language === 'zh' ? '疑似出货' : 'Distributing'
    case 'mixed':
      return language === 'zh' ? '多空混合' : 'Mixed'
    default:
      return language === 'zh' ? '数据不足' : 'Insufficient data'
  }
}

function getConfidenceLabel(confidence: DealerConfidence | undefined, language: string): string {
  switch (confidence) {
    case 'high':
      return language === 'zh' ? '高' : 'High'
    case 'medium':
      return language === 'zh' ? '中' : 'Medium'
    default:
      return language === 'zh' ? '低' : 'Low'
  }
}

function getDealerTone(direction: DealerDirection | undefined) {
  switch (direction) {
    case 'accumulating':
      return { border: 'border-emerald-300/25', bg: 'bg-emerald-300/[0.08]', text: 'text-emerald-200' }
    case 'distributing':
      return { border: 'border-red-300/25', bg: 'bg-red-300/[0.08]', text: 'text-red-200' }
    case 'mixed':
      return { border: 'border-amber-300/25', bg: 'bg-amber-300/[0.08]', text: 'text-amber-200' }
    default:
      return { border: 'border-zinc-300/20', bg: 'bg-zinc-300/[0.05]', text: 'text-zinc-300' }
  }
}

function getDealerToneClass(direction: DealerDirection | undefined): string {
  return getDealerTone(direction).text
}

function formatSigned(value: number): string {
  if (value > 0) return `+${value}`
  return String(value)
}

function formatTokenAmount(value: number): string {
  const abs = Math.abs(value)
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (abs >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toFixed(2)
}

function formatUSDT(value: number): string {
  const abs = Math.abs(value)
  if (abs >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (abs >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toFixed(2)
}

function formatQuote(value: number, quote: string): string {
  return `${formatUSDT(value)} ${quote}`
}

function formatSignedQuote(value: number, quote: string): string {
  const prefix = value > 0 ? '+' : ''
  return `${prefix}${formatUSDT(value)} ${quote}`
}

function formatSmallNumber(value: number): string {
  if (!Number.isFinite(value)) return '-'
  const abs = Math.abs(value)
  if (abs > 0 && abs < 0.000001) return value.toExponential(2)
  if (abs < 0.01) return value.toFixed(8).replace(/0+$/, '').replace(/\.$/, '')
  return value.toFixed(6).replace(/0+$/, '').replace(/\.$/, '')
}
