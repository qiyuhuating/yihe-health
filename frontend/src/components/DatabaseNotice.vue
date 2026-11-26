<template>
  <p v-if="!isOnline" class="database-notice" role="status" aria-live="polite">浏览器报告当前离线；部分联网功能可能受影响，请检查连接。已显示的页面内容仍可查看。</p>
  <p v-if="authState.databaseReady === false && authState.configError" class="database-notice database-notice--error" role="alert" aria-live="assertive">服务端配置有误，依赖数据库的功能不可用，自动重连不会恢复：{{ authState.configError }}</p>
  <p v-else-if="authState.databaseReady === false" class="database-notice" role="status" aria-live="polite">数据服务暂未连接；依赖数据库的功能暂不可用，正在自动连接。</p>
  <slot v-else />
</template>

<script setup lang="ts">
import { authState } from '../auth'
import { useNetworkStatus } from '../composables/useNetworkStatus'

const { isOnline } = useNetworkStatus()
</script>

<style scoped>
.database-notice {
  margin: 12px 0;
  padding: 10px 14px;
  border-left: 3px solid var(--care-warning);
  border-radius: var(--care-control-radius);
  background: var(--care-warning-bg);
  color: var(--care-warning-strong);
  font-size: 14px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}
.database-notice--error {
  border-left-color: var(--care-danger);
  background: var(--care-danger-bg);
  color: var(--care-danger);
}
</style>
