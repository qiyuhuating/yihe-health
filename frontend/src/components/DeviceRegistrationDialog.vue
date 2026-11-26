<template>
<el-dialog title="登记新设备" :model-value="visible" @update:model-value="onVisibleChange" width="min(560px, 94vw)" :close-on-click-modal="!creating" :close-on-press-escape="!creating" :show-close="!creating" @closed="emit('close')">
      <el-form v-if="!createdDevice" label-position="top" :model="form">
        <el-form-item label="别名">
          <el-input id="device-alias" v-model="form.alias" autocomplete="off" placeholder="如：爷爷的手表" maxlength="16"></el-input>
        </el-form-item>
        <el-form-item label="型号" for="device-model">
          <el-input id="device-model" v-model="form.model" placeholder="如：HarmonyOS-Wear"></el-input>
        </el-form-item>
        <el-form-item label="直接绑定老人">
          <el-select v-model="form.oldId" placeholder="可不选，之后用绑定码绑定" clearable style="width: 100%;">
            <el-option v-for="item in oldPeople" :key="item.oldId"
                       :label="item.name + '（编号 ' + item.oldId + '）'" :value="item.oldId"></el-option>
          </el-select>
        </el-form-item>
      </el-form>
      <div v-if="createdDevice" class="created">
        <div class="created-title">设备已登记，请把下面的信息填到手表端：</div>
        <div class="created-line">设备号：<b>{{ createdDevice.deviceNo }}</b>
          <el-button size="small" plain type="primary" @click="emit('copy', createdDevice.deviceNo)">复制</el-button>
        </div>
        <div class="created-line">密 钥：<b class="secret">{{ createdDevice.secret }}</b>
          <el-button size="small" plain type="primary" @click="emit('copy', createdDevice.secret)">复制</el-button>
        </div>
        <HarmonyWatchSetup :device-no="createdDevice.deviceNo" />
      </div>
      <template #footer><span  class="dialog-footer">
        <el-button :disabled="creating" @click="emit('close')">关 闭</el-button>
        <el-button v-if="!createdDevice" type="primary" :loading="creating" @click="emit('create')">登 记</el-button>
      </span></template>
    </el-dialog>
</template>

<script setup lang="ts">
import HarmonyWatchSetup from './HarmonyWatchSetup.vue'
import type { DeviceState, ResidentRecord, CreatedDeviceResponse } from '../composables/deviceModels'

defineProps<{
  visible: boolean
  creating: boolean
  form: DeviceState['createForm']
  oldPeople: ResidentRecord[]
  createdDevice: CreatedDeviceResponse | null
}>()
const emit = defineEmits<{
  close: []
  create: []
  copy: [text: string]
}>()
function onVisibleChange(visible: boolean) {
  if (!visible) emit('close')
}
</script>

<style scoped>
.created { margin-top: 10px; padding: 10px; background: #f5f7fa; border-radius: 6px; }
.created-title { font-size: 13px; color: #606266; padding-bottom: 6px; }
.created-line { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; font-size: 15px; line-height: 1.6; }
.secret { color: #52bab5; word-break: break-all; }
</style>
