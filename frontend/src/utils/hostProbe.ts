import type { HostProbeResult } from '@/types'
import { buildHttpURL, isIPv6Literal } from '@/utils/network'

const DEFAULT_PROBE_TIMEOUT_MS = 2500
const IPV6_PREFERENCE_BONUS_MS = 35
const STICKY_HOST_BONUS_MS = 20

function classifyHostFamily(host: string): 'ipv4' | 'ipv6' {
  return isIPv6Literal(host) ? 'ipv6' : 'ipv4'
}

function normalizeProbeError(err: unknown): string {
  if (err instanceof DOMException && err.name === 'AbortError') {
    return 'timeout'
  }
  if (err instanceof Error && err.message) {
    return err.message
  }
  return 'probe_failed'
}

async function probeHost(host: string, port: number, timeoutMs: number): Promise<HostProbeResult> {
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), timeoutMs)
  const startedAt = performance.now()

  try {
    const response = await fetch(buildHttpURL(host, port, `/health?ts=${Date.now()}`), {
      cache: 'no-store',
      mode: 'cors',
      signal: controller.signal,
    })
    const latencyMs = Math.round(performance.now() - startedAt)
    if (!response.ok) {
      return {
        host,
        family: classifyHostFamily(host),
        ok: false,
        latencyMs,
        error: `HTTP ${response.status}`,
      }
    }
    return {
      host,
      family: classifyHostFamily(host),
      ok: true,
      latencyMs,
    }
  } catch (err) {
    return {
      host,
      family: classifyHostFamily(host),
      ok: false,
      error: normalizeProbeError(err),
    }
  } finally {
    clearTimeout(timer)
  }
}

export async function probeHosts(
  hosts: string[],
  port: number,
  timeoutMs = DEFAULT_PROBE_TIMEOUT_MS,
): Promise<HostProbeResult[]> {
  const uniqueHosts = [...new Set(hosts.filter(Boolean))]
  return Promise.all(uniqueHosts.map((host) => probeHost(host, port, timeoutMs)))
}

export function pickPreferredHost(
  results: HostProbeResult[],
  currentHost?: string,
): HostProbeResult | null {
  const successful = results.filter((result) => result.ok)
  if (successful.length === 0) return null

  const ranked = [...successful].sort((a, b) => {
    const aScore =
      (a.latencyMs ?? Number.MAX_SAFE_INTEGER) -
      (a.family === 'ipv6' ? IPV6_PREFERENCE_BONUS_MS : 0) -
      (a.host === currentHost ? STICKY_HOST_BONUS_MS : 0)
    const bScore =
      (b.latencyMs ?? Number.MAX_SAFE_INTEGER) -
      (b.family === 'ipv6' ? IPV6_PREFERENCE_BONUS_MS : 0) -
      (b.host === currentHost ? STICKY_HOST_BONUS_MS : 0)
    return aScore - bScore
  })

  return ranked[0] ?? null
}
