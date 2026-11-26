<template>
  <section v-if="failed" class="view-error" role="alert" aria-live="assertive">
    <h2>当前页面暂时无法显示</h2>
    <p v-if="canRetry">可以重试（剩余 {{ maxRetries - attempt }} 次），或通过菜单打开其他页面。</p>
    <p v-else>连续重试仍未恢复，请通过菜单打开其他页面。</p>
    <button v-if="canRetry" type="button" :disabled="coolingDown" @click="retry">{{ coolingDown ? '正在准备重试…' : '重新加载当前页面' }}</button>
  </section>
  <slot v-else :key="attempt" />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onErrorCaptured, ref } from 'vue'
import { recoverFromChunkError } from '@/appGuards'

const maxRetries = 3
const retryCooldownMs = 1200
const failed = ref(false)
const attempt = ref(0)
const coolingDown = ref(false)
let cooldownTimer: ReturnType<typeof setTimeout> | null = null
const canRetry = computed(() => attempt.value < maxRetries)

function clearCooldownTimer() {
  if (cooldownTimer !== null) {
    clearTimeout(cooldownTimer)
    cooldownTimer = null
  }
}

onErrorCaptured((error, _instance, info) => {
  // Returning false stops the error before app.config.errorHandler (main.ts), so
  // report it here with the same prefix and keep its stale-deploy chunk recovery.
  console.error('[app]', info, error)
  recoverFromChunkError(error)
  failed.value = true
  if (attempt.value > 0 && canRetry.value) {
    clearCooldownTimer()
    coolingDown.value = true
    cooldownTimer = setTimeout(() => {
      coolingDown.value = false
      cooldownTimer = null
    }, retryCooldownMs)
  }
  return false
})

function retry() {
  if (!canRetry.value || coolingDown.value) return
  attempt.value++
  failed.value = false
}
onBeforeUnmount(clearCooldownTimer)
</script>

<style scoped>
.view-error { padding: 24px; color: var(--care-text); }
.view-error button { padding: 12px 18px; cursor: pointer; }
.view-error button:disabled { opacity: 0.6; cursor: not-allowed; }
</style>
