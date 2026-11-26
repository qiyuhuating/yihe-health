<template>
<el-dialog :close-on-click-modal="false" title="设备绑定码" :model-value="visible" @update:model-value="onVisibleChange" width="min(560px, 94vw)">
      <HarmonyWatchSetup v-if="currentDevice" :device-no="currentDevice.deviceNo" />
      <div v-if="currentDevice" class="tip">
        设备：{{ currentDevice.deviceNo }}（{{ currentDevice.alias }}）；
        当前绑定老人：{{ currentDevice.oldPersonName }}
      </div>
      <el-form class="binding-form" label-position="top">
        <el-form-item label="绑定老人">
          <el-select v-model="selectedResident" placeholder="请选择老人" style="width: 100%;">
            <el-option v-for="item in oldPeople" :key="item.oldId"
                       :label="item.name + '（编号 ' + item.oldId + '）'" :value="item.oldId"></el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="有效天数">
          <el-input v-model="expiryDays" inputmode="numeric" maxlength="3" placeholder="天数" style="width: 100%;"></el-input>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="emit('create')">生成绑定码</el-button>
        </el-form-item>
      </el-form>

      <div v-if="newBindCode" class="new-code">
        <div>生成成功，请在手表上输入：</div>
        <div class="code-text">{{ newBindCode.code }}
          <el-button size="small" plain type="primary" @click="emit('copy', newBindCode.code)">复制</el-button>
        </div>
        <div class="tip">有效期至 {{ newBindCode.expireTime }}，一个绑定码只能绑定一台手表</div>
      </div>

      <div class="binding-table-scroll" role="region" aria-label="已生成绑定码，可横向滚动" tabindex="0">
      <el-table class="binding-codes-table" :data="bindCodeList" size="small" border style="width: 100%;margin-top: 10px;">
        <el-table-column prop="code" label="绑定码" width="110" align="center"></el-table-column>
        <el-table-column prop="expireTime" label="过期时间" width="160" align="center"></el-table-column>
        <el-table-column label="状态" width="90" align="center">
          <template #default="scope" >
            <el-tag v-if="scope.row.used === 1" type="info" size="small">已使用</el-tag>
            <el-tag v-else type="success" size="small">未使用</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" align="center">
          <template #default="scope" >
            <el-button size="small" plain type="danger" :disabled="scope.row.used === 1"
                       @click="emit('delete', scope.row)">作废</el-button>
          </template>
        </el-table-column>
      </el-table>
      </div>
    </el-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import HarmonyWatchSetup from './HarmonyWatchSetup.vue'
import type { DeviceId, DeviceRecord, ResidentRecord, DeviceBindCode, NewBindCodeResponse } from '../composables/deviceModels'

const props = defineProps<{
  visible: boolean
  currentDevice: DeviceRecord | null
  oldPeople: ResidentRecord[]
  bindOldId: DeviceId
  bindExpireDays: string
  newBindCode: NewBindCodeResponse | null
  bindCodeList: DeviceBindCode[]
}>()
const emit = defineEmits<{
  close: []
  create: []
  delete: [code: DeviceBindCode]
  copy: [text: string]
  'update:bindOldId': [value: DeviceId]
  'update:bindExpireDays': [value: string]
}>()
const selectedResident = computed({
  get: () => props.bindOldId,
  set: value => emit('update:bindOldId', value)
})
const expiryDays = computed({
  get: () => props.bindExpireDays,
  set: value => emit('update:bindExpireDays', value)
})
function onVisibleChange(visible: boolean) {
  if (!visible) emit('close')
}
</script>

<style scoped>
.tip { color: var(--care-text-muted); font-size: 14px; line-height: 1.6; padding-top: 6px; }
.binding-form { display: grid; grid-template-columns: minmax(0, 1fr) 100px; gap: 0 12px; }
.binding-form :deep(.el-form-item:last-child) { grid-column: 1 / -1; }
@media (max-width: 480px) { .binding-form { grid-template-columns: minmax(0, 1fr); } }
.binding-table-scroll { max-width: 100%; overflow-x: auto; overscroll-behavior-inline: contain; }
.binding-codes-table { min-width: 480px; }
.new-code { padding: 10px; background: #f0f9f8; border-radius: 6px; }
.code-text { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; font-size: 24px; letter-spacing: 2px; color: var(--care-primary); font-weight: 700; padding: 8px 0; overflow-wrap: anywhere; }
</style>
