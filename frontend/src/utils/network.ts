export function isIPv6Literal(host: string): boolean {
  return host.includes(':') && !host.startsWith('[') && !host.endsWith(']')
}

export function formatHostForURL(host: string): string {
  return isIPv6Literal(host) ? `[${host}]` : host
}

export function buildHttpURL(host: string, port: number, path: string): string {
  return `http://${formatHostForURL(host)}:${port}${path}`
}

export function buildWsURL(host: string, port: number, path = '/ws'): string {
  return `ws://${formatHostForURL(host)}:${port}${path}`
}

export function parseHostPortInput(input: string, defaultPort = 55511): { host: string; port: number } {
  let value = input.trim()
  value = value.replace(/^ws:\/\/|^wss:\/\//, '')
  value = value.replace(/^http:\/\/|^https:\/\//, '')

  if (value.startsWith('[')) {
    const end = value.indexOf(']')
    if (end === -1) {
      return { host: value.slice(1), port: defaultPort }
    }
    const host = value.slice(1, end)
    const remainder = value.slice(end + 1)
    if (remainder.startsWith(':')) {
      const parsedPort = Number.parseInt(remainder.slice(1), 10)
      if (!Number.isNaN(parsedPort)) {
        return { host, port: parsedPort }
      }
    }
    return { host, port: defaultPort }
  }

  const colonCount = (value.match(/:/g) || []).length
  if (colonCount > 1) {
    return { host: value, port: defaultPort }
  }

  const colonIdx = value.lastIndexOf(':')
  if (colonIdx > 0) {
    const maybePort = value.slice(colonIdx + 1)
    if (/^\d+$/.test(maybePort)) {
      return {
        host: value.slice(0, colonIdx),
        port: Number.parseInt(maybePort, 10),
      }
    }
  }

  return { host: value, port: defaultPort }
}
