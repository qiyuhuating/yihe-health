<template>
  <div class="device">
    <header class="collection-heading"><div><h1>设备管理</h1><p>查看连接状态、跟进异常，并为家人关联照护设备。</p></div><el-button type="primary" aria-label="登记新设备" :icon="Plus" @click="createDialogVisible = true">登记新设备</el-button></header>
    <section class="collection-metrics" aria-label="设备摘要"><article><span>登记设备</span><strong>{{ deviceLoadError && !deviceList.length ? '—' : deviceTotal }}</strong><small>当前筛选中的设备总数</small></article><article><span>在线设备</span><strong>{{ deviceLoadError && !deviceList.length ? '—' : serverOnlineCount }}</strong><small>以最近一次设备连接状态为准</small></article><article><span>待处理异常</span><strong :class="alertsReady && alertTotals.total ? 'attention-number' : ''">{{ alertsReady ? alertTotals.total : '—' }}</strong><small>{{ alertsReady ? '人工确认与跟进后更新状态' : '异常统计暂未就绪' }}</small></article></section>
    <div v-if="deviceLoadError || alertLoadError" class="collection-feedback" role="alert"><span>{{ deviceLoadError || alertLoadError }}</span><el-button plain :loading="deviceLoading" @click="loadDevices">重新加载设备</el-button></div>
    <section class="device-alert-summary"><div><h2>异常跟进</h2><p v-if="alertsReady">人员 {{ alertTotals.personTotal }} · 心率 {{ alertTotals.rateTotal }} · 血压 {{ alertTotals.preTotal }}</p><p v-else>异常统计暂时无法确认，请重新加载。</p></div><el-button plain :disabled="!alertsReady || alertTotals.total === 0" @click="openWorkbench">查看全部待处理</el-button></section>
    <section class="collection-panel">
      <div class="collection-toolbar"><div class="collection-filters"><div><label for="device-query">查找设备</label><el-input id="device-query" v-model="deviceQuery" clearable placeholder="设备号、别名或长者姓名"></el-input></div><div><label for="device-state">连接状态</label><el-select id="device-state" v-model="deviceState"><el-option label="全部设备" value="all"></el-option><el-option label="在线" value="online"></el-option><el-option label="离线" value="offline"></el-option></el-select></div></div><div class="collection-tools"><el-button plain :loading="deviceLoading" @click="loadDevices">刷新</el-button><el-switch v-model="autoRefresh" active-text="自动刷新" aria-label="自动刷新设备列表"></el-switch></div></div>
      <p class="collection-caption" role="status">{{ autoRefresh ? (deviceLoadError || alertLoadError ? '网络波动，自动降低刷新频率并等待恢复' : '每5秒自动刷新') : '自动刷新已关闭' }} · 当前显示 {{ deviceList.length }} 台设备</p>
    <el-table v-if="!mobileLayout" class="device-desktop-table" v-loading="deviceLoading" :data="deviceList" style="width:100%">
      <template #empty><div  class="collection-empty"><strong>{{ deviceLoadError ? '设备列表暂时无法加载' : deviceList.length ? '没有匹配的设备' : '还没有登记设备' }}</strong><span>调整关键词或登记一台新的照护手表。</span></div></template>
      <el-table-column label="照护设备" min-width="180"><template #default="scope" ><strong class="device-table-name">{{ scope.row.alias || '未命名设备' }}</strong><small class="device-table-secondary">{{ scope.row.deviceNo }}</small></template></el-table-column>
      <el-table-column label="关联长者" min-width="105"><template #default="scope" >{{ scope.row.oldPersonName || '尚未绑定' }}</template></el-table-column>
      <el-table-column label="连接状态" width="90"><template #default="scope" ><el-tag v-if="scope.row.online === 1 || scope.row.online === '1'" type="success">在线</el-tag><el-tag v-else-if="scope.row.online === 0 || scope.row.online === '0'" type="info">离线</el-tag><span v-else>状态未知</span></template></el-table-column>
      <el-table-column label="待处理异常" width="210"><template #default="scope" ><div class="device-table-alerts"><el-button link :class="alertOf(scope.row.deviceId).personCount ? 'has-alert' : ''" :disabled="!alertsReady" @click="showAlert(scope.row, 'person')">人员 {{ alertsReady ? alertOf(scope.row.deviceId).personCount : '—' }}</el-button><el-button link :class="alertOf(scope.row.deviceId).rateCount ? 'has-alert' : ''" :disabled="!alertsReady" @click="showAlert(scope.row, 'rate')">心率 {{ alertsReady ? alertOf(scope.row.deviceId).rateCount : '—' }}</el-button><el-button link :class="alertOf(scope.row.deviceId).preCount ? 'has-alert' : ''" :disabled="!alertsReady" @click="showAlert(scope.row, 'pre')">血压 {{ alertsReady ? alertOf(scope.row.deviceId).preCount : '—' }}</el-button></div></template></el-table-column>
      <el-table-column label="电量" width="75"><template #default="scope" >{{ scope.row.battery == null ? '暂无' : scope.row.battery + '%' }}</template></el-table-column>
      <el-table-column label="最近上报" width="155"><template #default="scope" ><span class="device-table-time">{{ scope.row.lastTime || '暂无记录' }}</span></template></el-table-column>
      <el-table-column label="操作" width="190" fixed="right"><template #default="scope" ><div class="device-table-actions"><el-button type="primary" plain @click="openBindCodeDialog(scope.row)">绑定码</el-button><el-dropdown trigger="click" @command="handleDeviceCommand($event, scope.row)"><el-button plain aria-label="更多设备操作">更多 <el-icon aria-hidden="true"><ArrowDown /></el-icon></el-button><template #dropdown><el-dropdown-menu ><el-dropdown-item command="details">设备详情</el-dropdown-item><el-dropdown-item command="reset">重置密钥</el-dropdown-item><el-dropdown-item command="unbind" :disabled="!scope.row.oldId">解除绑定</el-dropdown-item><el-dropdown-item command="delete" divided>删除设备</el-dropdown-item></el-dropdown-menu></template></el-dropdown></div></template></el-table-column>
    </el-table>
    <el-pagination :current-page="currentPage" :page-size="pageSize" :total="deviceTotal" layout="total, prev, pager, next" @current-change="changeDevicePage" />
    <div v-if="mobileLayout && deviceList.length" class="device-mobile-grid"><article v-for="item in deviceList" :key="item.deviceId" class="device-record"><header><div><h3>{{ item.alias || '未命名设备' }}</h3><p>{{ item.deviceNo }}</p></div><span class="connection-state" :class="item.online == 1 ? 'is-online' : ''">{{ item.online == 1 ? '在线' : item.online == 0 ? '离线' : '状态未知' }}</span></header><dl><dt>绑定长者</dt><dd>{{ item.oldPersonName || '尚未绑定' }}</dd><dt>设备编号</dt><dd>{{ item.deviceId }}</dd><dt>剩余电量</dt><dd>{{ item.battery == null ? '暂无数据' : item.battery + '%' }}</dd><dt>最后上报</dt><dd>{{ item.lastTime || '暂无记录' }}</dd><dt>位置坐标</dt><dd>{{ item.lastLng == null || item.lastLat == null ? '暂无数据' : item.lastLng + '，' + item.lastLat }}</dd></dl><div class="device-alert-actions"><el-button plain :disabled="!alertsReady" @click="showAlert(item, 'person')">人员 · {{ alertsReady ? alertOf(item.deviceId).personCount : '—' }}</el-button><el-button plain :disabled="!alertsReady" @click="showAlert(item, 'rate')">心率 · {{ alertsReady ? alertOf(item.deviceId).rateCount : '—' }}</el-button><el-button plain :disabled="!alertsReady" @click="showAlert(item, 'pre')">血压 · {{ alertsReady ? alertOf(item.deviceId).preCount : '—' }}</el-button></div><div class="device-record-actions"><el-button type="primary" plain @click="openBindCodeDialog(item)">设备绑定码</el-button><el-button plain @click="resetSecret(item)">重置密钥</el-button><el-button plain :disabled="!item.oldId" @click="unbind(item)">解除绑定</el-button><el-button plain type="danger" @click="deleteDevice(item)">删除设备</el-button></div></article></div>
    <div v-else-if="mobileLayout" class="device-mobile-empty collection-empty"><strong>{{ deviceLoading ? '正在加载设备…' : deviceLoadError ? '设备列表暂时无法加载' : deviceList.length ? '没有匹配的设备' : '还没有登记设备' }}</strong><p>{{ deviceQuery || deviceState !== 'all' ? '调整关键词或连接状态，即可查看其他设备。' : '登记第一台手表，让照护信息开始连接。' }}</p></div>
    </section>
    <el-dialog title="设备详情" v-model="deviceDetailVisible"><dl v-if="detailDevice" class="device-detail-grid"><dt>设备别名</dt><dd>{{ detailDevice.alias || '未命名设备' }}</dd><dt>设备号</dt><dd>{{ detailDevice.deviceNo }}</dd><dt>设备编号</dt><dd>{{ detailDevice.deviceId }}</dd><dt>设备型号</dt><dd>{{ detailDevice.model || '暂无信息' }}</dd><dt>绑定长者</dt><dd>{{ detailDevice.oldPersonName || '尚未绑定' }}</dd><dt>最近上报</dt><dd>{{ detailDevice.lastTime || '暂无记录' }}</dd><dt>位置坐标</dt><dd>{{ detailDevice.lastLng == null || detailDevice.lastLat == null ? '暂无数据' : detailDevice.lastLng + '，' + detailDevice.lastLat }}</dd></dl><template #footer><span ><el-button type="primary" @click="deviceDetailVisible = false">关闭详情</el-button></span></template></el-dialog>
    <aside class="device-onboarding"><img src="/static/img/care-courtyard.webp" alt="" width="960" height="411" loading="lazy" decoding="async"><div><p class="collection-eyebrow">从一次连接开始</p><h2>让设备，成为关爱的延伸</h2><ol><li>登记手表，获取设备号与密钥。</li><li>在手表端配置连接信息。</li><li>生成绑定码，关联需要照护的长者。</li></ol><p class="collection-caption">设备注册后，请以实际上传记录确认连接是否正常。</p></div></aside>
    
    <DeviceAlertDialog :alert="alertDialog" @close="alertDialog.visible = false" @resolve="resolveAlert" @retry="retryAlertDetail" @navigate="gotoPage(alertDialog.kind)" />

    <DeviceAlertWorkbench :workbench="workbench" :is-resolving="isResolvingAlert" @close="closeWorkbench" @view="row => showAlert(row, row.kind || 'person')" @resolve="resolveAlertRow" />

    <DeviceRegistrationDialog
      :visible="createDialogVisible" :creating="creating" :form="createForm"
      :old-people="oldPeople" :created-device="createdDevice"
      @close="closeCreateDialog" @create="createDevice" @copy="copy"
    />
    <DeviceBindingDialog
      :visible="bindCodeDialogVisible" :current-device="currentDevice"
      :old-people="oldPeople" v-model:bind-old-id="bindOldId"
      v-model:bind-expire-days="bindExpireDays"
      :new-bind-code="newBindCode" :bind-code-list="bindCodeList"
      @close="bindCodeDialogVisible = false" @create="createBindCode"
      @delete="deleteBindCode" @copy="copy"
    />
  </div>
</template>

<script setup lang="ts">
import { ArrowDown, Plus } from '@element-plus/icons-vue'
import { useCollectionLayout } from '../composables/useCollectionLayout'
import { useDevices } from '../composables/useDevices'
import DeviceRegistrationDialog from '../components/DeviceRegistrationDialog.vue'
import DeviceBindingDialog from '../components/DeviceBindingDialog.vue'
import DeviceAlertDialog from '../components/DeviceAlertDialog.vue'
import DeviceAlertWorkbench from '../components/DeviceAlertWorkbench.vue'

const mobileLayout = useCollectionLayout()
const {
  deviceList, deviceTotal, currentPage, pageSize,
  deviceDetailVisible, detailDevice, deviceQuery, deviceState,
  deviceLoading, deviceLoadError, alertLoadError, alertsReady,
  creating, oldPeople, autoRefresh, createDialogVisible,
  bindCodeDialogVisible, createdDevice, currentDevice,
  bindCodeList, newBindCode, bindOldId, bindExpireDays,
  createForm, alertTotals, alertDialog, workbench,
  serverOnlineCount, loadDevices, changeDevicePage, handleDeviceCommand,
  alertOf, showAlert, retryAlertDetail, resolveAlert, resolveAlertRow, isResolvingAlert, openWorkbench, closeWorkbench, gotoPage,
  closeCreateDialog, createDevice, openBindCodeDialog,
  createBindCode, deleteBindCode, resetSecret, unbind, deleteDevice, copy
} = useDevices()
</script>

<style scoped src="./device.styles.css"></style>
