<template>
  <main class="not-found" role="main">
    <p class="not-found-code" aria-hidden="true">404</p>
    <h1>页面不存在</h1>
    <p>你访问的地址没有对应的页面，可能链接已失效或输入有误。</p>
    <el-button type="primary" @click="goHome">返回首页</el-button>
  </main>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { authState } from '../auth'

const router = useRouter()
function goHome () {
  const account = authState.account as { role?: string } | null
  void router.replace(!account ? '/sign-in' : account.role === 'FAMILY' ? '/family-portal/overview' : '/operations/overview')
}
</script>

<style scoped>
.not-found { min-height: 60vh; display: grid; place-content: center; justify-items: center; gap: 12px; padding: 32px 16px; text-align: center; color: var(--care-text); }
.not-found h1 { margin: 0; font-size: 22px; color: var(--care-heading); }
.not-found p { margin: 0; color: var(--care-text-secondary); }
.not-found .not-found-code { font-size: 56px; font-weight: 700; color: var(--care-primary); }
</style>
