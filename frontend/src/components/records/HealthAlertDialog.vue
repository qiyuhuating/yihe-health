<template>
  <el-dialog :title="dialog.title" v-model="dialog.visible" width="min(620px, 94vw)">
    <div v-if="dialog.loading" class="alert-loading" role="status">正在读取…</div>
    <div v-else-if="dialog.hasAlert === 0" class="no-alert" role="status">
      <el-icon class="no-alert-icon" aria-hidden="true"><CircleCheckFilled /></el-icon>
      <div class="no-alert-message">{{ dialog.message }}</div>
    </div>
    <div v-else>
      <el-alert :title="'问题：' + dialog.reason" type="error" :closable="false" show-icon></el-alert>
      <table class="alert-table">
        <tbody>
          <tr><th>老人</th><td>{{ dialog.oldPersonName }}</td></tr>
          <tr><th>本次测量</th><td>心率 {{ dialog.hr }} 次/分 血压 {{ dialog.bp }} mmHg</td></tr>
          <tr><th>发生时间</th><td>{{ dialog.occurTime }}</td></tr>
          <tr><th>判断依据</th><td>{{ dialog.detail }}</td></tr>
          <tr><th>怎么处理</th><td>{{ dialog.advice }}</td></tr>
        </tbody>
      </table>
      <div class="alert-actions">
        <el-button v-if="dialog.rateBad === 1" type="primary" :loading="dialog.resolving"
                   @click="emit('resolve', 'rate')">标记心率已处理</el-button>
        <el-button v-if="dialog.preBad === 1" type="primary" :loading="dialog.resolving"
                   @click="emit('resolve', 'pre')">标记血压已处理</el-button>
        <el-button plain @click="router.push('/operations/devices')">去设备管理看该手表</el-button>
      </div>
      <div class="tip-small">
        处理后这条记录会从「异常」变成「已处理」，红点消失，历史记录与曲线仍然保留。
        告警依据以当前监测规则及设备上报信息为准。需要核对阈值时，请查看监测阈值档案。
      </div>
    </div>
    <template #footer><span class="dialog-footer">
      <el-button @click="dialog.visible = false">关 闭</el-button>
    </span></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { CircleCheckFilled } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import type { VitalAlertState } from '@/domain/vitals'
defineProps<{ dialog: VitalAlertState }>()
const emit = defineEmits<{ resolve: [kind: 'rate' | 'pre'] }>()
const router = useRouter()
</script>

<style scoped>
.alert-loading {
  text-align: center;
  padding: 24px 16px;
  color: var(--care-text-muted);
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
.tip-small {
  margin-top: 10px;
  color: var(--care-text-muted);
  font-size: 14px;
  line-height: 1.7;
}
</style>
