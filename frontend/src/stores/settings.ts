import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { RelayConfig, Theme } from '@/types'
import { createDefaultRelayConfig, loadRelayConfig, saveRelayConfig } from '@/utils/relayConfig'

export const useSettingsStore = defineStore('settings', () => {
  const theme = ref<Theme>(
    (localStorage.getItem('wt-theme') as Theme) || 'dark'
  )
  const username = ref(localStorage.getItem('wt-username') || '')
  const relayConfig = ref<RelayConfig>(loadRelayConfig())

  function applyTheme(t: Theme) {
    const html = document.documentElement
    if (t === 'dark') {
      html.classList.add('dark')
    } else {
      html.classList.remove('dark')
    }
  }

  function setTheme(t: Theme) {
    theme.value = t
    localStorage.setItem('wt-theme', t)
    applyTheme(t)
  }

  function toggleTheme() {
    setTheme(theme.value === 'dark' ? 'light' : 'dark')
  }

  function setUsername(name: string) {
    username.value = name
    localStorage.setItem('wt-username', name)
  }

  function setRelaySettings(config: RelayConfig) {
    relayConfig.value = { ...config }
    saveRelayConfig(relayConfig.value)
  }

  function resetRelaySettings() {
    setRelaySettings(createDefaultRelayConfig())
  }

  applyTheme(theme.value)

  return {
    theme, username, relayConfig,
    setTheme, toggleTheme, setUsername,
    setRelaySettings, resetRelaySettings,
  }
})
