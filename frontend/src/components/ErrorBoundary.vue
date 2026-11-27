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
.view-error {
  max-width: 600px;
  margin: 24px auto;
  padding: 28px;
  border: 1px solid var(--care-danger-border);
  border-radius: var(--care-panel-radius);
  background: var(--care-surface);
  box-shadow: var(--care-shadow-sm);
  color: var(--care-text);
}
.view-error h2 {
  margin: 0 0 10px;
  color: var(--care-heading);
  font-size: 20px;
}
.view-error p {
  margin: 0 0 18px;
  color: var(--care-text-secondary);
  line-height: 1.75;
}
.view-error button {
  min-height: var(--care-target-primary);
  padding: 10px 20px;
  border: 1px solid var(--care-primary);
  border-radius: var(--care-control-radius);
  background: var(--care-primary);
  color: #ffffff;
  font-weight: 500;
  cursor: pointer;
}
.view-error button:hover:not(:disabled) {
  background: var(--care-primary-hover);
  border-color: var(--care-primary-hover);
}
.view-error button:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
</style>
