<template>
  <div class="app-root">
    <care-config><router-view /></care-config>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { monitorDatabase } from './auth'
const router = useRouter()
let stopMonitoring: (() => void) | undefined
onMounted(() => { stopMonitoring = monitorDatabase(router) })
onBeforeUnmount(() => stopMonitoring?.())
</script>

<style src="./styles/app-foundation.css"></style>
<style src="./styles/app-components.css"></style>
<style src="./styles/app-family-responsive.css"></style>
