<template>
  <el-dialog :title="alert.title" :model-value="alert.visible" @update:model-value="onVisibleChange" width="min(620px, 94vw)">
    <div v-if="alert.loading" class="alert-loading" role="status">正在读取…</div>
    <div v-else-if="alert.hasAlert === -1" class="no-alert" role="status">
      <p>{{ alert.message }}</p>
      <el-button type="primary" plain @click="emit('retry')">重新读取详情</el-button>
    </div>
    <div v-else-if="alert.hasAlert === 0" class="no-alert" role="status">
      <el-icon class="no-alert-icon" aria-hidden="true"><CircleCheckFilled /></el-icon>
      <div class="no-alert-message">{{ alert.message }}</div>
    </div>
    <div v-else>
      <el-alert :title="'问题：' + alert.reason" type="error" :closable="false" show-icon></el-alert>

      <table class="alert-table">
        <tbody>
          <tr><th>设备</th><td>{{ alert.deviceNo }}（{{ alert.alias }}）</td></tr>
          <tr><th>绑定老人</th><td>{{ alert.oldPersonName }}</td></tr>
          <tr v-if="alert.value !== ''"><th>本次测量</th><td>{{ alert.value }}</td></tr>
          <tr v-if="alert.threshold"><th>正常范围</th><td>{{ alert.threshold }}</td></tr>
          <tr><th>发生时间</th><td>{{ alert.occurTime }}</td></tr>
          <tr><th>判断依据</th><td>{{ alert.detail }}</td></tr>
          <tr><th>怎么处理</th><td>{{ alert.advice }}</td></tr>
        </tbody>
      </table>

      <div class="alert-actions">
        <el-button type="primary" :loading="alert.resolving" @click="emit('resolve')">标记已处理（消除红点）</el-button>
        <el-button plain @click="emit('navigate')">去相关页面看看</el-button>
      </div>
      <div class="tip">
        「标记已处理」表示这个问题已经人工确认/跟进过了：设备类问题会记录为已处理，
        心率血压会同时把这条记录从"异常"改为"已处理"，红点随之消失，历史记录仍然保留。
      </div>
    </div>
    <template #footer><span class="dialog-footer">
      <el-button @click="emit('close')">关 闭</el-button>
    </span></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { CircleCheckFilled } from '@element-plus/icons-vue'
import type { DeviceAlert } from '../composables/deviceModels'

defineProps<{ alert: DeviceAlert }>()
const emit = defineEmits<{
  close: []
  resolve: []
  navigate: []
  retry: []
}>()

function onVisibleChange(visible: boolean) {
  if (!visible) emit('close')
}
</script>

<style scoped>
.alert-loading {
  text-align: center;
  padding: 24px 16px;
  color: var(--care-text-muted);
}
.alert-table {
  width: 100%;
  margin-top: 14px;
  border-collapse: collapse;
  font-size: 15px;
}
.alert-table th {
  width: 96px;
  text-align: left;
  color: var(--care-text-secondary);
  font-weight: 600;
  padding: 10px 12px;
  vertical-align: top;
  background: var(--care-surface-muted);
  border: 1px solid var(--care-border);
}
.alert-table td {
  padding: 10px 12px;
  color: var(--care-text);
  border: 1px solid var(--care-border);
  line-height: 1.65;
  overflow-wrap: anywhere;
}
.alert-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 16px;
}
.no-alert {
  text-align: center;
  padding: 24px 16px;
  color: var(--care-text-secondary);
}
.no-alert-icon {
  color: var(--care-primary);
  font-size: 32px;
}
.no-alert-message {
  margin-top: 8px;
}
.tip {
  color: var(--care-text-muted);
  font-size: 14px;
  line-height: 1.7;
  padding-top: 10px;
}
</style>
