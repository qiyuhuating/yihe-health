<template>
  <section class="harmony-setup" aria-label="鸿蒙手表接入">
    <h3>鸿蒙手表连接与授权</h3>
    <el-radio-group v-model="mode" aria-label="手表运行环境">
      <el-radio-button value="device">真机</el-radio-button>
      <el-radio-button value="emulator">DevEco 模拟器</el-radio-button>
    </el-radio-group>
    <label :for="inputId">手表可访问的后台地址</label>
    <el-input v-if="mode === 'device'" :id="inputId" v-model="server" placeholder="https://care.example.com" autocomplete="off" />
    <p v-else>http://127.0.0.1:18796（需通过 HDC 转发至电脑后端）</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <el-button v-if="address" plain @click="copyAddress">复制后台地址</el-button>
    <p v-if="copied" role="status">{{ copied }}</p>
    <ol>
      <li>在手表“设置”中填入上述地址、设备号 <strong>{{ deviceNo }}</strong> 和登记时显示的设备密钥，点击“保存地址并认证”。</li>
      <li>在后台选择长者并生成设备绑定码，在手表输入该码后点击“绑定长者”；家属邀请码不能用于设备绑定。</li>
      <li>佩戴手表，在首页点击“授权并读取传感器”，分别同意心率和活动权限；在“记录”页上传，家属首页即可查看最近健康采样。</li>
    </ol>
    <p class="harmony-note">真机须能访问该 HTTPS 地址，电脑的 127.0.0.1 不代表手表能访问电脑。传感器授权在手表系统弹窗完成；本页不会替用户授权。</p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, useId } from 'vue'
defineProps<{ deviceNo: string }>()
const inputId = useId()
const mode = ref('device')
const server = ref(location.protocol === 'https:' ? location.origin : '')
const copied = ref('')
const address = computed(() => {
  if (mode.value === 'emulator') return 'http://127.0.0.1:18796'
  try {
    const url = new URL(server.value.trim())
    if (url.protocol !== 'https:' || ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname) || url.username || url.password || url.pathname !== '/' || url.search || url.hash) return ''
    return url.origin
  } catch { return '' }
})
const error = computed(() => mode.value === 'device' && !address.value ? '请填写手表可访问的 HTTPS 主机地址，不含账号、路径或查询参数。' : '')
async function copyAddress() {
  try { await navigator.clipboard.writeText(address.value); copied.value = '后台地址已复制，请填入手表设置。' }
  catch { copied.value = '复制未完成，请手动选择地址复制。' }
}
</script>

<style scoped>
.harmony-setup { margin-top: 20px; padding-top: 16px; border-top: 1px solid #dce4da; line-height: 1.8; overflow-wrap: anywhere; }
.harmony-setup h3 { margin: 0 0 12px; font-size: 18px; color: var(--care-heading); }
.harmony-setup label { display: block; margin: 12px 0 6px; }
.harmony-setup ol { padding-left: 22px; }
.harmony-setup li { margin-bottom: 8px; }
.harmony-setup [role="alert"] { color: var(--care-danger); }
.harmony-note { color: var(--care-text-muted); font-size: 14px; }
</style>
