<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoomStore } from '@/stores/room'
import { useSettingsStore } from '@/stores/settings'
import {
  ArrowRight,
  Key,
  Globe,
  AlertCircle,
  Loader2,
  ChevronDown,
  ShieldCheck,
  RotateCcw,
  Wifi,
  Radio,
} from 'lucide-vue-next'
import { createDefaultRelayConfig } from '@/utils/relayConfig'
import type { LANRoomInfo } from '@/types'

const room = useRoomStore()
const settings = useSettingsStore()

const displayName = ref(settings.username || '')
const primaryMode = ref<'create' | 'join'>('create')
const joinMode = ref<'passcode' | 'ip' | 'lan' | 'relay'>('passcode')
const passcodeInput = ref('')
const ipInput = ref('')
const showJoinAdvanced = ref(false)
const showAdvanced = ref(false)
const relayCreateEnabled = ref(false)
const signalingRelayUrl = ref(localStorage.getItem('wt-signaling-relay-url') || '')
const signalingRelayRoomId = ref('')

const relayForm = ref({
  enabled: settings.relayConfig.enabled,
  stunUrlsText: settings.relayConfig.stunUrls.join('\n'),
  turnUrlsText: settings.relayConfig.turnUrls.join('\n'),
  username: settings.relayConfig.username,
  credential: settings.relayConfig.credential,
})

const trimmedName = computed(() => displayName.value.trim())
const canCreate = computed(() => !!trimmedName.value && !room.isConnecting)
const canJoinDirect = computed(() => {
  if (!trimmedName.value || room.isConnecting) return false
  if (joinMode.value === 'passcode') return !!passcodeInput.value.trim()
  if (joinMode.value === 'ip') return !!ipInput.value.trim()
  return false
})
const canJoinRelay = computed(() =>
  !!trimmedName.value &&
  !!signalingRelayUrl.value.trim() &&
  !!signalingRelayRoomId.value.trim() &&
  !room.isConnecting
)
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

function setJoinMode(mode: 'passcode' | 'ip' | 'lan' | 'relay') {
  joinMode.value = mode
  if (mode !== 'passcode') {
    showJoinAdvanced.value = true
  }
}

async function handleCreate() {
  if (!trimmedName.value) return
  if (relayCreateEnabled.value && signalingRelayUrl.value.trim()) {
    localStorage.setItem('wt-signaling-relay-url', signalingRelayUrl.value.trim())
    await room.createRoomViaSignalingRelay(trimmedName.value, signalingRelayUrl.value.trim())
    return
  }
  await room.createRoom(trimmedName.value)
}

async function handleJoin() {
  if (!trimmedName.value) return
  if (joinMode.value === 'passcode') {
    if (!passcodeInput.value.trim()) return
    await room.joinRoomByPasscode(passcodeInput.value, trimmedName.value)
    return
  }
  if (joinMode.value === 'ip') {
    if (!ipInput.value.trim()) return
    await room.joinRoomByIP(ipInput.value, trimmedName.value)
  }
}

async function handleDiscoverLAN() {
  await room.discoverLANRooms()
}

async function handleJoinLAN(roomInfo: LANRoomInfo) {
  if (!trimmedName.value) return
  await room.joinLANRoom(roomInfo, trimmedName.value)
}

async function handleJoinRelay() {
  if (!trimmedName.value || !signalingRelayUrl.value.trim() || !signalingRelayRoomId.value.trim()) return
  localStorage.setItem('wt-signaling-relay-url', signalingRelayUrl.value.trim())
  await room.joinRoomViaSignalingRelay(
    signalingRelayUrl.value.trim(),
    signalingRelayRoomId.value.trim(),
    trimmedName.value,
  )
}

const flowBodyContentRef = ref<HTMLElement | null>(null)
const flowBodyHeight = ref<number | null>(null)
const flowBodyAnimReady = ref(false)
let flowBodyObserver: ResizeObserver | null = null

function syncFlowBodyHeight() {
  if (!flowBodyContentRef.value) return
  flowBodyHeight.value = flowBodyContentRef.value.scrollHeight
}

onMounted(async () => {
  await nextTick()
  syncFlowBodyHeight()
  if (!flowBodyContentRef.value) return

  flowBodyObserver = new ResizeObserver(() => {
    syncFlowBodyHeight()
  })
  flowBodyObserver.observe(flowBodyContentRef.value)

  requestAnimationFrame(() => {
    flowBodyAnimReady.value = true
  })
})

onBeforeUnmount(() => {
  flowBodyObserver?.disconnect()
})

watch(
  [primaryMode, showJoinAdvanced, joinMode, () => room.lanRooms.length, () => room.lanDiscovering],
  async () => {
    await nextTick()
    syncFlowBodyHeight()
  },
)

const flowBodyShellStyle = computed(() =>
  flowBodyHeight.value !== null ? { height: `${flowBodyHeight.value}px` } : undefined,
)
</script>

<template>
  <div class="home-shell h-full w-full overflow-hidden">
    <div class="home-layout h-full w-full">
      <section class="home-brand-stage relative flex min-h-0 items-center overflow-hidden">
        <div class="home-brand-backdrop" aria-hidden="true">
          <div class="home-brand-backdrop__mesh" />
          <div class="home-brand-backdrop__grid" />
          <div class="home-brand-backdrop__dots" />
          <div class="home-brand-backdrop__accent" />
          <div class="home-brand-backdrop__glow" />
        </div>
        <div class="home-brand relative z-[1] w-full max-w-2xl">
            <div class="text-[11px] uppercase tracking-[0.34em] text-fg-subtle">
              Private room playback
            </div>
            <h1 class="home-brand__title mt-4 text-fg">
              Watch Together
            </h1>
            <p class="home-brand__slogan mt-5 max-w-lg text-fg-muted">
              和朋友开一个同步房间。房主创建后分享口令，其他人输入口令就能马上加入。
            </p>
          </div>
      </section>

      <section class="home-panel min-h-0">
          <div class="border-b border-line pb-5">
            <div class="text-xs font-semibold uppercase tracking-[0.24em] text-fg-subtle">
              当前身份
            </div>
            <label class="mt-3 block">
              <div class="mb-2 text-sm font-medium text-fg">你的昵称</div>
              <input
                v-model="displayName"
                type="text"
                maxlength="20"
                placeholder="输入一个在房间里显示的名字"
                class="input h-12 px-4 text-base"
                @keyup.enter="primaryMode === 'create' ? handleCreate() : handleJoin()"
              />
            </label>
          </div>

          <section class="home-flow mt-5">
            <div class="home-mode-tabs">
              <button
                class="home-mode-tab"
                :class="primaryMode === 'create'
                  ? 'home-mode-tab--active'
                  : 'text-fg-muted hover:text-fg'"
                @click="primaryMode = 'create'"
              >
                我要开房
              </button>
              <button
                class="home-mode-tab"
                :class="primaryMode === 'join'
                  ? 'home-mode-tab--active'
                  : 'text-fg-muted hover:text-fg'"
                @click="primaryMode = 'join'"
              >
                我要加入
              </button>
            </div>
            <div
              class="home-flow__body-shell"
              :class="{ 'home-flow__body-shell--animated': flowBodyAnimReady }"
              :style="flowBodyShellStyle"
            >
              <div ref="flowBodyContentRef" class="home-flow__body">
              <div v-if="primaryMode === 'create'" class="space-y-5">
                <div>
                  <div class="text-sm font-semibold text-fg">标准创建</div>
                  <p class="mt-2 text-sm leading-6 text-fg-muted">
                    程序会在本机创建房间，并给你生成可直接分享的口令。
                  </p>
                  <button
                    class="btn mt-5 h-12 w-full text-base"
                    :disabled="!canCreate"
                    @click="handleCreate"
                  >
                    <Loader2 v-if="room.isConnecting" :size="18" class="animate-spin" />
                    <span>创建房间</span>
                    <ArrowRight :size="18" />
                  </button>
                </div>
              </div>

              <div v-else class="space-y-5">
                <div>
                  <div class="flex items-center gap-2 text-sm font-semibold text-fg">
                    <Key :size="16" />
                    口令加入
                  </div>
                  <p class="mt-2 text-sm leading-6 text-fg-muted">
                    房主把口令发给你时，直接填这里即可。
                  </p>
                  <input
                    v-model="passcodeInput"
                    type="text"
                    placeholder="输入房主分享的口令"
                    class="input mt-4 px-4 font-mono tracking-wider"
                    @focus="setJoinMode('passcode')"
                    @keyup.enter="handleJoin"
                  />
                  <button
                    class="btn mt-4 h-12 w-full text-base"
                    :disabled="!(trimmedName && passcodeInput.trim()) || room.isConnecting"
                    @click="setJoinMode('passcode'); handleJoin()"
                  >
                    <Loader2 v-if="room.isConnecting && joinMode === 'passcode'" :size="18" class="animate-spin" />
                    <span>加入房间</span>
                    <ArrowRight :size="18" />
                  </button>
                </div>

                <section class="border-t border-line pt-4">
                  <button
                    class="flex w-full items-center justify-between text-left transition-fast hover:text-fg"
                    @click="showJoinAdvanced = !showJoinAdvanced"
                  >
                    <div>
                      <div class="text-sm font-medium text-fg">更多加入方式</div>
                      <div class="mt-1 text-xs text-fg-subtle">
                        局域网扫描、直接 IP 或信令中继
                      </div>
                    </div>
                    <ChevronDown
                      :size="16"
                      class="transition-transform"
                      :class="{ 'rotate-180': showJoinAdvanced }"
                    />
                  </button>

                  <div v-if="showJoinAdvanced" class="space-y-4 pt-4">
                    <div class="grid gap-2 sm:grid-cols-3">
                      <button
                        class="border px-3 py-2 text-left transition-fast min-w-0"
                        :class="joinMode === 'lan'
                          ? 'border-line-strong bg-bg-sunken text-fg'
                          : 'border-line text-fg-muted hover:text-fg'"
                        @click="setJoinMode('lan')"
                      >
                        <div class="flex items-center gap-2 text-sm font-medium">
                          <Wifi :size="14" />
                          局域网
                        </div>
                        <div class="mt-1 text-xs leading-5 text-fg-subtle">
                          自动发现同网房间
                        </div>
                      </button>
                      <button
                        class="border px-3 py-2 text-left transition-fast min-w-0"
                        :class="joinMode === 'ip'
                          ? 'border-line-strong bg-bg-sunken text-fg'
                          : 'border-line text-fg-muted hover:text-fg'"
                        @click="setJoinMode('ip')"
                      >
                        <div class="flex items-center gap-2 text-sm font-medium">
                          <Globe :size="14" />
                          IP 加入
                        </div>
                        <div class="mt-1 text-xs leading-5 text-fg-subtle">
                          已知地址时直接连
                        </div>
                      </button>
                      <button
                        class="border px-3 py-2 text-left transition-fast min-w-0"
                        :class="joinMode === 'relay'
                          ? 'border-line-strong bg-bg-sunken text-fg'
                          : 'border-line text-fg-muted hover:text-fg'"
                        @click="setJoinMode('relay')"
                      >
                        <div class="flex items-center gap-2 text-sm font-medium">
                          <Radio :size="14" />
                          中继加入
                        </div>
                        <div class="mt-1 text-xs leading-5 text-fg-subtle">
                          适合跨网络入房
                        </div>
                      </button>
                    </div>

                    <div v-if="joinMode === 'lan'" class="space-y-3 border border-line bg-bg-base/40 p-4">
                      <button
                        class="btn-outline h-11 w-full"
                        :disabled="room.lanDiscovering"
                        @click="handleDiscoverLAN"
                      >
                        <Loader2 v-if="room.lanDiscovering" :size="16" class="animate-spin" />
                        <Radio v-else :size="16" />
                        <span>扫描局域网房间</span>
                      </button>
                      <div v-if="room.lanRooms.length > 0" class="space-y-2">
                        <button
                          v-for="item in room.lanRooms"
                          :key="`${item.roomId}-${item.port}`"
                          class="flex w-full items-center justify-between gap-3 border border-line bg-bg-elevated/70 px-3 py-3 text-left transition-fast hover:bg-bg-elevated"
                          :disabled="!trimmedName || room.isConnecting"
                          @click="handleJoinLAN(item)"
                        >
                          <span class="min-w-0">
                            <span class="block truncate text-sm font-medium text-fg">
                              {{ item.username || '局域网房间' }}
                            </span>
                            <span class="block truncate font-mono text-[11px] text-fg-subtle">
                              {{ item.roomId }} · {{ item.ips[0] || item.from }}:{{ item.port }}
                            </span>
                          </span>
                          <ArrowRight :size="15" class="shrink-0 text-fg-muted" />
                        </button>
                      </div>
                      <div v-else class="text-xs leading-5 text-fg-muted">
                        扫描后会显示同一局域网内正在广播的房间。
                      </div>
                    </div>

                    <div v-else-if="joinMode === 'ip'" class="space-y-3 border border-line bg-bg-base/40 p-4">
                      <input
                        v-model="ipInput"
                        type="text"
                        class="input"
                        placeholder="房主 IP 或 IP:端口"
                        @keyup.enter="handleJoin"
                      />
                      <button
                        class="btn h-11 w-full"
                        :disabled="!canJoinDirect"
                        @click="handleJoin"
                      >
                        <Loader2 v-if="room.isConnecting && joinMode === 'ip'" :size="16" class="animate-spin" />
                        <span>通过 IP 加入</span>
                        <ArrowRight :size="16" />
                      </button>
                    </div>

                    <div v-else-if="joinMode === 'relay'" class="space-y-3 border border-line bg-bg-base/40 p-4">
                      <input
                        v-model="signalingRelayUrl"
                        type="text"
                        class="input text-sm"
                        placeholder="wss://你的中继服务器/ws"
                      />
                      <input
                        v-model="signalingRelayRoomId"
                        type="text"
                        class="input font-mono text-sm"
                        placeholder="12 位安全邀请码"
                        @keyup.enter="handleJoinRelay"
                      />
                      <button
                        class="btn h-11 w-full"
                        :disabled="!canJoinRelay"
                        @click="handleJoinRelay"
                      >
                        <Loader2 v-if="room.isConnecting && joinMode === 'relay'" :size="16" class="animate-spin" />
                        <span>通过中继加入</span>
                        <ArrowRight :size="16" />
                      </button>
                      <div class="text-xs leading-5 text-fg-muted">
                        输入房主分享的 12 位安全邀请码。它同时是入房凭证，不会暴露房主地址。
                      </div>
                    </div>
                  </div>
                </section>
              </div>
              </div>
            </div>
          </section>

          <div
            v-if="room.connectionError"
            class="mt-5 flex items-start gap-2 border border-danger/30 bg-danger/5 px-4 py-3 text-sm text-danger"
          >
            <AlertCircle :size="16" class="mt-0.5 shrink-0" />
            <span>{{ room.connectionError }}</span>
          </div>

          <section class="mt-5 overflow-hidden border-t border-line">
            <button
              class="flex w-full items-center justify-between py-4 text-left transition-fast hover:text-fg"
              @click="showAdvanced = !showAdvanced"
            >
              <div>
                <div class="flex items-center gap-2 text-sm font-medium text-fg">
                  <ShieldCheck :size="15" />
                  高级网络与中继
                </div>
                <div class="mt-1 text-xs text-fg-subtle">
                  合并信令中继创建与 STUN / TURN / TURNS 配置
                </div>
              </div>
              <ChevronDown
                :size="16"
                class="transition-transform"
                :class="{ 'rotate-180': showAdvanced }"
              />
            </button>

            <div v-if="showAdvanced" class="space-y-5 border-t border-line pt-4">
              <div class="space-y-3 border-b border-line pb-5">
                <div class="text-xs font-semibold uppercase tracking-[0.22em] text-fg-subtle">
                  信令中继创建
                </div>
                <label class="flex items-center gap-2 text-sm text-fg">
                  <input v-model="relayCreateEnabled" type="checkbox" />
                  创建房间时通过信令中继
                </label>
                <input
                  v-if="relayCreateEnabled"
                  v-model="signalingRelayUrl"
                  type="text"
                  class="input text-sm"
                  placeholder="wss://你的中继服务器/ws"
                />
                <div class="text-xs leading-5 text-fg-muted">
                  只有明确要走公网信令时再打开。普通同网使用不需要这里。
                </div>
              </div>

              <div class="space-y-4">
                <div class="text-xs font-semibold uppercase tracking-[0.22em] text-fg-subtle">
                  STUN / TURN / TURNS
                </div>
                <label class="flex items-center gap-2 text-sm text-fg">
                  <input v-model="relayForm.enabled" type="checkbox" />
                  启用 TURN / TURNS 中继
                </label>

                <div class="grid gap-4 lg:grid-cols-2">
                  <div class="space-y-2">
                    <div class="text-xs text-fg-subtle">STUN 地址，每行一个</div>
                    <textarea
                      v-model="relayForm.stunUrlsText"
                      class="input min-h-28 font-mono text-xs resize-y"
                      placeholder="stun:stun.l.google.com:19302"
                    />
                  </div>
                  <div class="space-y-2">
                    <div class="text-xs text-fg-subtle">TURN / TURNS 地址，每行一个</div>
                    <textarea
                      v-model="relayForm.turnUrlsText"
                      class="input min-h-28 font-mono text-xs resize-y"
                      placeholder="turns:your-domain:30001?transport=tcp"
                    />
                  </div>
                </div>

                <div class="grid gap-3 sm:grid-cols-2">
                  <input
                    v-model="relayForm.username"
                    type="text"
                    class="input text-sm"
                    placeholder="TURN 用户名"
                  />
                  <input
                    v-model="relayForm.credential"
                    type="password"
                    class="input text-sm"
                    placeholder="TURN 密码 / credential"
                  />
                </div>

                <div class="border border-line bg-bg-base/40 px-4 py-3 text-xs leading-5 text-fg-muted">
                  推荐在受限网络下使用 `turns:域名:30001?transport=tcp`。这里接的是标准 TURN / TURNS，
                  没有内置 frp 进程。
                </div>

                <div class="flex flex-col gap-2 sm:flex-row">
                  <button class="btn h-11 flex-1" @click="saveRelaySettings">
                    <ShieldCheck :size="15" />
                    <span>保存网络配置</span>
                  </button>
                  <button class="btn-outline h-11" @click="resetRelaySettings">
                    <RotateCcw :size="15" />
                    <span>重置</span>
                  </button>
                </div>
              </div>
            </div>
          </section>

          <div class="home-panel__spacer" aria-hidden="true" />
      </section>
    </div>
  </div>
</template>

<style scoped>
.home-shell {
  background: var(--color-bg-base);
}

.home-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.618fr) minmax(0, 1fr);
  height: 100%;
  min-height: 0;
}

.home-brand-stage {
  padding: clamp(2rem, 5vh, 4rem) clamp(2rem, 5vw, 5rem);
  border-right: 1px solid var(--color-line);
  background: color-mix(in srgb, var(--color-bg-sunken) 42%, var(--color-bg-base));
}

.home-brand-backdrop {
  position: absolute;
  inset: 0;
  overflow: hidden;
  pointer-events: none;
}

.home-brand-backdrop__mesh {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(ellipse 95% 75% at 12% 38%, color-mix(in srgb, var(--color-fg) 11%, transparent), transparent 62%),
    radial-gradient(ellipse 70% 55% at 78% 72%, color-mix(in srgb, var(--color-fg) 7%, transparent), transparent 58%),
    radial-gradient(ellipse 50% 40% at 55% 18%, color-mix(in srgb, var(--color-fg) 4%, transparent), transparent 70%),
    linear-gradient(
      148deg,
      color-mix(in srgb, var(--color-bg-sunken) 72%, transparent) 0%,
      transparent 38%,
      color-mix(in srgb, var(--color-bg-base) 48%, transparent) 100%
    );
}

.home-brand-backdrop__grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(color-mix(in srgb, var(--color-fg) 7%, transparent) 1px, transparent 1px),
    linear-gradient(90deg, color-mix(in srgb, var(--color-fg) 7%, transparent) 1px, transparent 1px);
  background-size: 48px 48px;
  opacity: 0.55;
  mask-image: linear-gradient(135deg, black 0%, black 72%, transparent 100%);
}

.home-brand-backdrop__dots {
  position: absolute;
  inset: -15%;
  background-image: radial-gradient(
    color-mix(in srgb, var(--color-fg) 16%, transparent) 1.25px,
    transparent 1.25px
  );
  background-size: 18px 18px;
  opacity: 0.7;
  mask-image: radial-gradient(ellipse 92% 88% at 32% 46%, black 22%, transparent 88%);
}

.home-brand-backdrop__accent {
  position: absolute;
  inset: -10%;
  background:
    repeating-linear-gradient(
      -14deg,
      transparent 0,
      transparent 30px,
      color-mix(in srgb, var(--color-fg) 9%, transparent) 30px,
      color-mix(in srgb, var(--color-fg) 9%, transparent) 31px
    );
  opacity: 0.6;
  mask-image: linear-gradient(90deg, black 0%, black 78%, transparent 100%);
}

.home-brand-backdrop__glow {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(ellipse 55% 45% at 8% 88%, color-mix(in srgb, var(--color-fg) 6%, transparent), transparent 68%),
    linear-gradient(90deg, color-mix(in srgb, var(--color-fg) 5%, transparent), transparent 28%);
  opacity: 0.85;
}

.home-panel {
  display: flex;
  flex-direction: column;
  justify-content: flex-start;
  height: 100%;
  padding: clamp(1.25rem, 3vh, 2.5rem) clamp(1.5rem, 4vw, 3.5rem);
  overflow-y: auto;
  background: var(--color-bg-elevated);
}

.home-panel__spacer {
  flex: 1 1 auto;
  min-height: 0;
}

.home-flow {
  border: 1px solid var(--color-line);
  border-radius: 0.875rem;
  overflow: hidden;
  background: color-mix(in srgb, var(--color-bg-base) 35%, var(--color-bg-elevated));
}

.home-flow__body-shell {
  overflow: hidden;
}

.home-flow__body-shell--animated {
  transition: height 280ms cubic-bezier(0.4, 0, 0.2, 1);
}

.home-flow__body {
  padding: clamp(1rem, 2vw, 1.4rem);
}

.home-mode-tabs {
  display: flex;
  gap: 0.375rem;
  padding: 0.5rem;
  border-bottom: 1px solid var(--color-line);
  background: color-mix(in srgb, var(--color-bg-sunken) 50%, var(--color-bg-elevated));
}

.home-mode-tab {
  flex: 1;
  padding: 0.55rem 0.75rem;
  border-radius: 0.5rem;
  text-align: center;
  font-size: 0.875rem;
  font-weight: 600;
  transition: all 160ms ease;
}

.home-mode-tab--active {
  background: var(--color-bg-elevated);
  color: var(--color-fg);
  box-shadow: 0 1px 2px color-mix(in srgb, var(--color-fg) 8%, transparent);
}

.home-brand__title {
  font-family: var(--font-display);
  font-size: clamp(3.25rem, 6.5vw, 6.5rem);
  font-weight: 600;
  line-height: 0.92;
  letter-spacing: -0.05em;
}

.home-brand__slogan {
  font-size: clamp(0.95rem, 1.2vw, 1.125rem);
  line-height: 1.8;
}

@media (max-width: 1024px) {
  .home-layout {
    grid-template-columns: 1fr;
    grid-template-rows: auto 1fr;
    overflow-y: auto;
  }

  .home-brand-stage {
    min-height: clamp(16rem, 38vh, 24rem);
    border-right: none;
    border-bottom: 1px solid var(--color-line);
  }

  .home-panel {
    height: auto;
    min-height: 0;
    flex: 1;
    justify-content: flex-start;
  }
}

@media (max-width: 640px) {
  .home-brand-stage {
    padding: 1.5rem 1.25rem;
    min-height: clamp(14rem, 34vh, 20rem);
  }

  .home-panel {
    padding: 1.25rem;
  }

  .home-brand__title {
    font-size: clamp(2.75rem, 14vw, 4rem);
  }
}
</style>
