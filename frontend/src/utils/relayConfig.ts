import type { RelayConfig } from '@/types'

export const RELAY_CONFIG_KEY = 'wt-relay-config'

const DEFAULT_STUN_URLS = [
  'stun:stun.l.google.com:19302',
  'stun:stun1.l.google.com:19302',
]

export function createDefaultRelayConfig(): RelayConfig {
  return {
    enabled: false,
    stunUrls: [...DEFAULT_STUN_URLS],
    turnUrls: [],
    username: '',
    credential: '',
  }
}

function normalizeUrlList(values: string[] | undefined, fallback: string[] = []): string[] {
  const normalized = (values || [])
    .map((value) => value.trim())
    .filter(Boolean)
  if (normalized.length > 0) return [...new Set(normalized)]
  return [...fallback]
}

export function normalizeRelayConfig(input?: Partial<RelayConfig> | null): RelayConfig {
  const defaults = createDefaultRelayConfig()
  return {
    enabled: Boolean(input?.enabled),
    stunUrls: normalizeUrlList(input?.stunUrls, defaults.stunUrls),
    turnUrls: normalizeUrlList(input?.turnUrls),
    username: (input?.username || '').trim(),
    credential: input?.credential || '',
  }
}

export function loadRelayConfig(): RelayConfig {
  const raw = localStorage.getItem(RELAY_CONFIG_KEY)
  if (!raw) return createDefaultRelayConfig()

  try {
    const parsed = JSON.parse(raw) as Partial<RelayConfig>
    return normalizeRelayConfig(parsed)
  } catch {
    return createDefaultRelayConfig()
  }
}

export function saveRelayConfig(config: RelayConfig) {
  localStorage.setItem(RELAY_CONFIG_KEY, JSON.stringify(normalizeRelayConfig(config)))
}
