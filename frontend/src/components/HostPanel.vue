<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoomStore } from '@/stores/room'
import { useSettingsStore } from '@/stores/settings'
import { createDefaultRelayConfig } from '@/utils/relayConfig'
import type { RoomPasscode } from '@/types'
import {
  Link, Upload, Copy, Check, AlertTriangle, Info, FileVideo, Globe, ShieldCheck, Wifi, ChevronDown, RotateCcw,
} from 'lucide-vue-next'

const room = useRoomStore()
const settings = useSettingsStore()

const urlInput = ref('')
const copiedIp = ref<string | null>(null)
const copiedAll = ref(false)
const showRelaySettings = ref(false)
const showAllPasscodes = ref(false)
const relayForm = ref({
  enabled: settings.relayConfig.enabled,
  stunUrlsText: settings.relayConfig.stunUrls.join('\n'),
  turnUrlsText: settings.relayConfig.turnUrls.join('\n'),
  username: settings.relayConfig.username,
  credential: settings.relayConfig.credential,
})

async function handleSetURL() {
  if (!urlInput.value.trim()) return
  await room.setVideoURL(urlInput.value)
  urlInput.value = ''
}

async function copyPasscode(ip: string, passcode: string) {
  try {
    await navigator.clipboard.writeText(passcode)
    copiedIp.value = ip
    setTimeout(() => { copiedIp.value = null }, 2000)
  } catch {}
}

async function copyAll() {
  const text = room.passcodes
    .map(p => `${p.ip}: ${p.passcode}`)
    .join('\n')
  try {
    await navigator.clipboard.writeText(text)
    copiedAll.value = true
    setTimeout(() => { copiedAll.value = false }, 2000)
  } catch {}
}

const shareAddressOptions = computed(() => room.passcodes)

async function copySecureInvite() {
  if (!room.secureInvite) return
  try {
    await navigator.clipboard.writeText(room.secureInvite.code)
    copiedIp.value = 'secure-invite'
    setTimeout(() => { copiedIp.value = null }, 2000)
  } catch {}
}

const primaryPasscodes = computed(() => {
  const items = room.passcodes
  const selected: RoomPasscode[] = []
  const seen = new Set<string>()

  const pushIfPresent = (item?: RoomPasscode) => {
    if (!item || seen.has(item.ip)) return
    selected.push(item)
    seen.add(item.ip)
  }

  pushIfPresent(
    items.find(item => item.isIPv6Public && item.isIPv6Temporary)
      || items.find(item => item.isIPv6Public),
  )
  pushIfPresent(items.find(item => !item.isIPv6Public && !item.isIPv6ULA))

  if (selected.length === 0) pushIfPresent(items[0])
  if (selected.length === 1) pushIfPresent(items.find(item => !seen.has(item.ip)))

  return selected
})

const extraPasscodes = computed(() => {
  const visible = new Set(primaryPasscodes.value.map(item => item.ip))
  return room.passcodes.filter(item => !visible.has(item.ip))
})

const visiblePasscodes = computed(() => {
  if (showAllPasscodes.value) {
    return [...primaryPasscodes.value, ...extraPasscodes.value]
  }
  return primaryPasscodes.value
})

const selectedShareAddress = computed(() =>
  room.passcodes.find(item => item.ip === room.preferredShareIp),
)

function passcodeBadgeText(item: RoomPasscode) {
  if (item.isIPv6Public && item.isIPv6Temporary) return '临时 IPv6'
  if (item.isIPv6Public) return '公网 IPv6'
  if (item.isIPv6ULA) return 'ULA'
  return 'IPv4'
}

function passcodeBadgeTitle(item: RoomPasscode) {
  if (item.isIPv6Public && item.isIPv6Temporary) {
    return '临时公网 IPv6，默认优先展示，用于降低暴露固定地址的风险'
  }
  if (item.isIPv6Public) {
    return '公网 IPv6，可跨网络直连，但通常比临时地址更稳定'
  }
  if (item.isIPv6ULA) {
    return 'ULA 地址，仅限局域网或虚拟局域网内访问'
  }
  return 'IPv4 地址，通常适用于同一局域网或经虚拟组网访问'
}

function passcodeBadgeClass(item: RoomPasscode) {
  if (item.isIPv6Public && item.isIPv6Temporary) {
    return 'passcode-badge passcode-badge--temp'
  }
  if (item.isIPv6Public) {
    return 'passcode-badge passcode-badge--public'
  }
  if (item.isIPv6ULA) {
    return 'passcode-badge passcode-badge--ula'
  }
  return 'passcode-badge passcode-badge--ipv4'
}

function passcodeBadgeIcon(item: RoomPasscode) {
  if (item.isIPv6ULA) return Wifi
  if (item.isIPv6Public) return ShieldCheck
  return Globe
}

function saveRelaySettings() {
  settings.setRelaySettings({
    enabled: relayForm.value.enabled,
    stunUrls: relayForm.value.stunUrlsText.split(/\r?\n/),
    turnUrls: relayForm.value.turnUrlsText.split(/\r?\n/),
    username: relayForm.value.username.trim(),
    credential: relayForm.value.credential,
  })
  relayForm.value = {
    enabled: settings.relayConfig.enabled,
    stunUrlsText: settings.relayConfig.stunUrls.join('\n'),
    turnUrlsText: settings.relayConfig.turnUrls.join('\n'),
    username: settings.relayConfig.username,
    credential: settings.relayConfig.credential,
  }
}

function resetRelaySettings() {
  const defaults = createDefaultRelayConfig()
  settings.resetRelaySettings()
  relayForm.value = {
    enabled: defaults.enabled,
    stunUrlsText: defaults.stunUrls.join('\n'),
    turnUrlsText: defaults.turnUrls.join('\n'),
    username: defaults.username,
    credential: defaults.credential,
  }
}
</script>

<template>
  <div class="h-full overflow-y-auto">
    <!-- Video source -->
    <section class="p-4 border-b border-line">
      <h3 class="text-xs font-semibold uppercase tracking-wider text-fg-subtle mb-3">
        视频源
      </h3>

      <!-- Current source -->
      <div v-if="room.videoState.source" class="mb-3 px-3 py-2 bg-bg-sunken rounded text-xs">
        <div class="flex items-center gap-1.5 text-fg-muted mb-1">
          <FileVideo :size="12" />
          当前视频
        </div>
        <div class="text-fg-muted truncate font-mono">{{ room.videoState.source }}</div>
        <div class="text-fg-subtle mt-0.5">类型: {{ room.videoState.sourceType }}</div>
        <div v-if="room.videoState.chunkManifest" class="text-fg-subtle mt-0.5 truncate font-mono">
          分发清单: {{ room.videoState.chunkManifest }}
        </div>
      </div>

      <!-- Direct URL input -->
      <div class="flex gap-2 mb-2">
        <input
          v-model="urlInput"
          type="text"
          placeholder="直接输入视频 URL 或 magnet 链接"
          class="input flex-1 text-xs"
          @keyup.enter="handleSetURL"
        />
        <button class="btn px-3 text-xs" :disabled="!urlInput.trim()" @click="handleSetURL">
          <Link :size="13" />
          设置
        </button>
      </div>

      <!-- Local file -->
      <button class="btn-outline w-full text-xs" @click="room.selectLocalVideoFile()">
        <Upload :size="13" />
        {{ room.localFilePreparing ? '正在预处理本地视频...' : '选择本地视频文件' }}
      </button>

      <div
        v-if="room.localFileProgress"
        class="mt-2 rounded border border-line bg-bg-sunken px-3 py-2 text-xs"
      >
        <div class="flex items-center justify-between mb-1 text-fg-muted">
          <span>
            本地文件预处理：
            {{ room.localFileProgress.stage === 'encoding' ? '预分块中' :
              room.localFileProgress.stage === 'hashing' ? '计算校验中' :
              room.localFileProgress.stage === 'complete' ? '完成' :
              room.localFileProgress.stage === 'error' ? '失败' : '准备中' }}
          </span>
          <span>{{ Math.round(room.localFileProgress.percent) }}%</span>
        </div>
        <div class="h-1.5 rounded bg-bg overflow-hidden">
          <div
            class="h-full bg-accent transition-all duration-200"
            :style="{ width: `${Math.max(0, Math.min(100, room.localFileProgress.percent))}%` }"
          />
        </div>
      </div>
    </section>

    <!-- Room info -->
    <section class="host-section border-b border-line">
      <h3 class="host-section__title">
        房间信息
      </h3>

      <div class="host-meta-grid">
        <div class="host-meta-card">
          <div class="host-meta-card__label">房间号</div>
          <div class="host-meta-card__value font-mono">{{ room.roomId }}</div>
        </div>
        <div class="host-meta-card">
          <div class="host-meta-card__label">端口</div>
          <div class="host-meta-card__value flex items-center gap-1.5 font-mono">
            <span>{{ room.serverPort }}</span>
            <span
              v-if="!room.isDefaultPort"
              class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-2xs text-danger"
            >
              <AlertTriangle :size="10" />
              非默认
            </span>
          </div>
        </div>
      </div>

      <div
        v-if="room.passcodes.length > 1"
        class="host-share-card mt-4"
      >
        <div class="host-share-card__label">本地视频对外共享地址</div>
        <select
          :value="room.preferredShareIp"
          class="input mt-2 w-full py-2.5 text-sm"
          @change="room.setPreferredShareIp(($event.target as HTMLSelectElement).value)"
        >
          <option
            v-for="item in shareAddressOptions"
            :key="item.ip"
            :value="item.ip"
          >
            {{ passcodeBadgeText(item) }}
          </option>
        </select>
        <div
          v-if="selectedShareAddress"
          class="host-share-card__ip mt-2.5"
          :title="selectedShareAddress.ip"
        >
          {{ selectedShareAddress.ip }}
        </div>
        <p class="host-share-card__hint mt-2.5">
          默认优先选择临时 IPv6。若对方当前网络无法访问，再手动切换到其他地址。
        </p>
      </div>

      <div
        v-if="!room.isDefaultPort"
        class="host-warning-banner mt-4"
      >
        <AlertTriangle :size="14" class="shrink-0 mt-0.5" />
        <span>
          当前端口 {{ room.serverPort }} 非默认端口 55511。
          请确保对方使用正确的端口或口令连接。
        </span>
      </div>
    </section>

    <!-- Passcodes -->
    <section class="host-section">
      <div class="flex items-center justify-between gap-3 mb-4">
        <h3 class="host-section__title mb-0">
          连接口令
        </h3>
        <button
          v-if="room.passcodes.length > 1"
          class="host-copy-all"
          @click="copyAll"
        >
          <Check v-if="copiedAll" :size="12" />
          <Copy v-else :size="12" />
          {{ copiedAll ? '已复制' : '复制全部' }}
        </button>
      </div>

      <div v-if="room.passcodes.length === 0" class="text-sm text-fg-subtle">
        正在生成口令...
      </div>

      <div v-if="room.secureInvite" class="mt-4 border border-line bg-bg-base/40 p-3">
        <div class="text-xs font-medium text-fg">安全邀请码（通过信令中继加入）</div>
        <div class="mt-2 flex items-center gap-2">
          <code class="min-w-0 flex-1 truncate font-mono text-sm tracking-wider text-fg">{{ room.secureInvite.code }}</code>
          <button class="btn-outline px-2 py-1 text-xs" @click="copySecureInvite">
            <Check v-if="copiedIp === 'secure-invite'" :size="13" />
            <Copy v-else :size="13" />
            复制
          </button>
        </div>
        <div class="mt-2 text-xs text-fg-subtle">请同时把已配置的信令中继地址告诉对方。</div>
      </div>

      <div v-else class="space-y-3">
        <article
          v-for="item in visiblePasscodes"
          :key="item.ip"
          class="passcode-card"
          :class="{ 'passcode-card--highlight': item.isIPv6Public && item.isIPv6Temporary }"
        >
          <div class="passcode-card__header">
            <span
              :class="passcodeBadgeClass(item)"
              :title="passcodeBadgeTitle(item)"
            >
              <component :is="passcodeBadgeIcon(item)" :size="11" />
              {{ passcodeBadgeText(item) }}
            </span>
            <button
              class="passcode-card__copy"
              :title="`复制 ${passcodeBadgeText(item)} 口令`"
              @click="copyPasscode(item.ip, item.passcode)"
            >
              <Check v-if="copiedIp === item.ip" :size="14" />
              <Copy v-else :size="14" />
            </button>
          </div>

          <div class="passcode-card__ip" :title="item.ip">
            {{ item.ip }}
          </div>

          <div class="passcode-card__code">
            <div class="passcode-card__code-label">口令</div>
            <code class="passcode-card__code-value">{{ item.passcode }}</code>
          </div>
        </article>

        <button
          v-if="extraPasscodes.length > 0"
          class="host-expand-btn"
          @click="showAllPasscodes = !showAllPasscodes"
        >
          <span>{{ showAllPasscodes ? '收起其他地址' : `展开其他地址（${extraPasscodes.length}）` }}</span>
          <ChevronDown
            :size="14"
            class="shrink-0 transition-transform"
            :class="{ 'rotate-180': showAllPasscodes }"
          />
        </button>
      </div>

      <div class="host-info-tip mt-4">
        <Info :size="13" class="shrink-0 mt-0.5" />
        <span>
          默认只显示一个临时 IPv6 和一个 IPv4 口令，便于直接分享。
          如果需要固定公网 IPv6、ULA 或虚拟局域网地址，再展开其他地址。
        </span>
      </div>
    </section>

    <section class="host-section border-t border-line">
      <button
        class="w-full flex items-center justify-between px-3 py-2.5 text-left rounded bg-bg-sunken hover:bg-bg transition-fast"
        @click="showRelaySettings = !showRelaySettings"
      >
        <span class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-fg-subtle">
          <ShieldCheck :size="13" />
          TURN / TURNS 中继
        </span>
        <ChevronDown
          :size="15"
          class="text-fg-muted transition-transform"
          :class="{ 'rotate-180': showRelaySettings }"
        />
      </button>

      <div v-if="showRelaySettings" class="mt-3 space-y-3">
        <label class="flex items-center gap-2 text-xs text-fg">
          <input v-model="relayForm.enabled" type="checkbox" />
          启用 TURN/TURNS 中继
        </label>

        <div class="space-y-1">
          <div class="text-2xs text-fg-subtle">STUN 地址，每行一个</div>
          <textarea
            v-model="relayForm.stunUrlsText"
            class="input w-full text-xs font-mono resize-y min-h-18"
            placeholder="stun:stun.l.google.com:19302"
          />
        </div>

        <div class="space-y-1">
          <div class="text-2xs text-fg-subtle">TURN / TURNS 地址，每行一个</div>
          <textarea
            v-model="relayForm.turnUrlsText"
            class="input w-full text-xs font-mono resize-y min-h-18"
            placeholder="turns:your-domain:30001?transport=tcp"
          />
        </div>

        <input
          v-model="relayForm.username"
          type="text"
          class="input text-xs"
          placeholder="TURN 用户名"
        />
        <input
          v-model="relayForm.credential"
          type="password"
          class="input text-xs"
          placeholder="TURN 密码 / credential"
        />

        <div class="text-2xs text-fg-subtle leading-5">
          推荐在受限网络下使用 `turns:域名:30001?transport=tcp`。
          该配置会在新的 WebRTC 连接创建时生效。
        </div>

        <div class="flex gap-2">
          <button class="btn flex-1 text-xs" @click="saveRelaySettings">
            <ShieldCheck :size="13" />
            保存中继配置
          </button>
          <button class="btn-outline text-xs" @click="resetRelaySettings">
            <RotateCcw :size="13" />
            重置
          </button>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.host-section {
  padding: 1.125rem 1rem;
}

.host-section__title {
  margin-bottom: 0.875rem;
  font-size: 0.6875rem;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--color-fg-subtle);
}

.host-meta-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
}

.host-meta-card {
  padding: 0.75rem 0.875rem;
  border: 1px solid var(--color-line);
  border-radius: 0.625rem;
  background: color-mix(in srgb, var(--color-bg-sunken) 55%, var(--color-bg-elevated));
}

.host-meta-card__label {
  font-size: 0.6875rem;
  color: var(--color-fg-subtle);
}

.host-meta-card__value {
  margin-top: 0.375rem;
  font-size: 0.9375rem;
  font-weight: 500;
  color: var(--color-fg);
}

.host-share-card {
  padding: 0.875rem;
  border: 1px solid var(--color-line);
  border-radius: 0.625rem;
  background: color-mix(in srgb, var(--color-bg-sunken) 40%, var(--color-bg-elevated));
}

.host-share-card__label {
  font-size: 0.75rem;
  font-weight: 500;
  color: var(--color-fg);
}

.host-share-card__ip {
  padding: 0.625rem 0.75rem;
  border-radius: 0.5rem;
  background: var(--color-bg-sunken);
  font-family: ui-monospace, monospace;
  font-size: 0.6875rem;
  line-height: 1.5;
  color: var(--color-fg-muted);
  word-break: break-all;
}

.host-share-card__hint {
  font-size: 0.6875rem;
  line-height: 1.55;
  color: var(--color-fg-subtle);
}

.host-warning-banner {
  display: flex;
  align-items: flex-start;
  gap: 0.625rem;
  padding: 0.75rem 0.875rem;
  border: 1px solid color-mix(in srgb, var(--color-danger-rgb) 30%, transparent);
  border-radius: 0.625rem;
  font-size: 0.75rem;
  line-height: 1.55;
  color: var(--color-danger);
  background: color-mix(in srgb, var(--color-danger-rgb) 6%, transparent);
}

.host-copy-all {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.375rem 0.625rem;
  border-radius: 0.5rem;
  font-size: 0.6875rem;
  color: var(--color-fg-muted);
  transition: color 120ms ease, background-color 120ms ease;
}

.host-copy-all:hover {
  color: var(--color-fg);
  background: var(--color-bg-sunken);
}

.passcode-card {
  padding: 0.875rem;
  border: 1px solid var(--color-line);
  border-radius: 0.625rem;
  background: var(--color-bg-elevated);
}

.passcode-card--highlight {
  border-color: color-mix(in srgb, var(--color-accent) 28%, var(--color-line));
  background: color-mix(in srgb, var(--color-accent) 4%, var(--color-bg-elevated));
}

.passcode-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.passcode-badge {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  padding: 0.25rem 0.625rem;
  border-radius: 999px;
  font-size: 0.6875rem;
  font-weight: 500;
  line-height: 1.2;
}

.passcode-badge--temp {
  color: var(--color-accent);
  background: color-mix(in srgb, var(--color-accent) 10%, transparent);
}

.passcode-badge--public {
  color: var(--color-fg-muted);
  background: var(--color-bg-sunken);
}

.passcode-badge--ula,
.passcode-badge--ipv4 {
  color: var(--color-fg-subtle);
  background: var(--color-bg-sunken);
}

.passcode-card__copy {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0.375rem;
  border-radius: 0.5rem;
  color: var(--color-fg-muted);
  transition: color 120ms ease, background-color 120ms ease;
}

.passcode-card__copy:hover {
  color: var(--color-fg);
  background: var(--color-bg-sunken);
}

.passcode-card__ip {
  margin-top: 0.75rem;
  font-family: ui-monospace, monospace;
  font-size: 0.6875rem;
  line-height: 1.55;
  color: var(--color-fg-muted);
  word-break: break-all;
}

.passcode-card__code {
  margin-top: 0.75rem;
  padding: 0.75rem;
  border-radius: 0.5rem;
  background: var(--color-bg-sunken);
}

.passcode-card__code-label {
  margin-bottom: 0.375rem;
  font-size: 0.6875rem;
  color: var(--color-fg-subtle);
}

.passcode-card__code-value {
  display: block;
  font-family: ui-monospace, monospace;
  font-size: 0.8125rem;
  line-height: 1.6;
  letter-spacing: 0.04em;
  color: var(--color-fg);
  word-break: break-all;
}

.host-expand-btn {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.75rem 0.875rem;
  border: 1px solid var(--color-line);
  border-radius: 0.625rem;
  font-size: 0.75rem;
  text-align: left;
  color: var(--color-fg-muted);
  transition: color 120ms ease, border-color 120ms ease, background-color 120ms ease;
}

.host-expand-btn:hover {
  border-color: var(--color-line-strong);
  color: var(--color-fg);
  background: var(--color-bg-sunken);
}

.host-info-tip {
  display: flex;
  align-items: flex-start;
  gap: 0.625rem;
  padding: 0.75rem 0.875rem;
  border-radius: 0.625rem;
  font-size: 0.6875rem;
  line-height: 1.6;
  color: var(--color-fg-subtle);
  background: color-mix(in srgb, var(--color-bg-sunken) 65%, var(--color-bg-elevated));
}
</style>
