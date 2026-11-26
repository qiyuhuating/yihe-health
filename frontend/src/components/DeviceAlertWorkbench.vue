<template>
<el-dialog title="待处理异常" :model-value="workbench.visible" @update:model-value="onVisibleChange" width="min(760px, 94vw)">
      <div class="tip" role="status">{{ workbench.loading ? '正在读取异常详情，已完成 ' + workbench.list.length + ' 条…' : '共 ' + workbench.list.length + ' 条未处理异常。' }}点「查看原因」看具体问题和处理办法，处理完点「标记已处理」消除红点。</div>
      <el-table v-loading="workbench.loading && !workbench.list.length" :data="workbench.list" size="small" border style="width:100%;margin-top:8px;" :aria-busy="workbench.loading">
        <template #empty><div class="empty-alerts">{{ workbench.loading ? '正在查找待处理异常…' : '当前没有待处理异常' }}</div></template>
        <el-table-column prop="deviceNo" label="设备号" width="120" align="center"></el-table-column>
        <el-table-column prop="alias" label="别名" width="110" align="center"></el-table-column>
        <el-table-column label="问题" width="120" align="center">
          <template #default="scope" >
            <el-tag type="danger" size="small">{{ scope.row.kindText }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="reason" label="原因" align="left"></el-table-column>
        <el-table-column label="操作" width="170" align="center">
          <template #default="scope" >
            <el-button size="small" plain type="primary" @click="emit('view', scope.row)">查看原因</el-button>
            <el-button size="small" plain :loading="isResolving(scope.row)" :disabled="isResolving(scope.row)" @click="emit('resolve', scope.row)">标记已处理</el-button>
          </template>
        </el-table-column>
      </el-table>
      <template #footer><span  class="dialog-footer">
        <el-button @click="emit('close')">关 闭</el-button>
      </span></template>
    </el-dialog>
</template>

<script setup lang="ts">
import type { DeviceRecord, DeviceState } from '../composables/deviceModels'

defineProps<{ workbench: DeviceState['workbench']; isResolving: (row: DeviceRecord) => boolean }>()
const emit = defineEmits<{
  close: []
  view: [row: DeviceRecord]
  resolve: [row: DeviceRecord]
}>()

function onVisibleChange(visible: boolean) {
  if (!visible) emit('close')
}
</script>

<style scoped>
.tip { color: var(--care-text-muted); font-size: 14px; line-height: 1.65; padding-top: 6px; }
.empty-alerts { padding: 20px 8px; font-size: 14px; color: var(--care-text-muted); }
</style>
