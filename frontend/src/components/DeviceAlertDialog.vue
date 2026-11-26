<template>
<el-dialog :title="alert.title" :model-value="alert.visible" @update:model-value="onVisibleChange" width="min(620px, 94vw)">
      <div v-if="alert.loading" style="text-align:center;padding:20px;">正在读取 ...</div>
      <div v-else-if="alert.hasAlert === -1" class="no-alert" role="status">
        <p>{{ alert.message }}</p>
        <el-button type="primary" plain @click="emit('retry')">重新读取详情</el-button>
      </div>
      <div v-else-if="alert.hasAlert === 0" class="no-alert">
        <el-icon style="color:#52BAB5;font-size:32px;"><CircleCheckFilled /></el-icon>
        <div style="margin-top:8px;">{{ alert.message }}</div>
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
      <template #footer><span  class="dialog-footer">
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
.alert-table {
  width: 100%;
  margin-top: 12px;
  border-collapse: collapse;
  font-size: 15px;
}
.alert-table th {
  width: 90px;
  text-align: left;
  color: #909399;
  font-weight: normal;
  padding: 6px 8px;
  vertical-align: top;
  background: #fafafa;
  border: 1px solid #ebeef5;
}
.alert-table td {
  padding: 6px 8px;
  color: #303133;
  border: 1px solid #ebeef5;
  line-height: 20px;
}
.alert-actions { margin-top: 14px; }
.no-alert { text-align: center; padding: 20px; color: #606266; }
.tip { color: var(--care-text-muted); font-size: 14px; line-height: 1.65; padding-top: 6px; }
</style>
