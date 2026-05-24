export interface SystemConfig {
  initialized: boolean
  beta_mode?: boolean
  data_gateway_url?: string
}

const DEFAULT_SYSTEM_CONFIG: SystemConfig = {
  initialized: true,
}

let configPromise: Promise<SystemConfig> | null = null
let cachedConfig: SystemConfig | null = null

export function getSystemConfig(): Promise<SystemConfig> {
  if (cachedConfig) {
    return Promise.resolve(cachedConfig)
  }
  if (configPromise) {
    return configPromise
  }
  configPromise = fetch('/api/config')
    .then(readSystemConfigResponse)
    .then((data) => {
      cachedConfig = data
      return data
    })
  return configPromise
}

async function readSystemConfigResponse(res: Response): Promise<SystemConfig> {
  if (!res.ok) {
    return DEFAULT_SYSTEM_CONFIG
  }

  const text = await res.text()
  if (!text.trim()) {
    return DEFAULT_SYSTEM_CONFIG
  }

  try {
    const data = JSON.parse(text) as Partial<SystemConfig>
    return {
      ...data,
      initialized: data.initialized ?? DEFAULT_SYSTEM_CONFIG.initialized,
    }
  } catch {
    return DEFAULT_SYSTEM_CONFIG
  }
}

/** Call after first-time setup completes so next check reflects initialized=true */
export function invalidateSystemConfig() {
  cachedConfig = null
  configPromise = null
  window.dispatchEvent(new Event('system-config-invalidated'))
}
