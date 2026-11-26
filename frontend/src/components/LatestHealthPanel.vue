<template>
  <section class="latest-health" aria-labelledby="latest-health-heading" :aria-busy="loading">
    <div class="latest-heading">
      <div><h2 id="latest-health-heading">最近健康采样</h2><p>各来源分别展示，按采样开始时间选取；历史记录保留原时间。</p></div>
      <div class="latest-actions">
        <el-button v-if="isSupported && !loading && !error" link :aria-pressed="isSpeaking" @click="toggle(digest.spokenText)">
          {{ isSpeaking ? '停止播报' : '语音播报' }}
        </el-button>
        <el-button link :loading="loading" @click="load">刷新采样</el-button>
      </div>
    </div>
    <div v-if="!loading && !error" class="digest-banner" :class="'digest-banner--' + digest.status" >
      <div class="digest-header">
        <strong>{{ digest.headline }}</strong>
        <span class="digest-freshness">{{ digest.freshnessText }}</span>
      </div>
      <p class="digest-summary">{{ digest.summary }}{{ digest.suggestedAction }}</p>
    </div>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-else-if="loading" role="status">正在读取最近采样…</p>
    <p v-else-if="!items.length" class="latest-empty">暂无健康采样；日常上报记录仍可在心率、睡眠与步数页面查看。</p>
    <ul v-else class="latest-list">
      <li v-for="item in items" :key="item.sampleId">
        <h3>{{ metricLabels[item.sample.metric] || item.sample.metric }}</h3>
        <p class="latest-value">{{ item.sample.value == null ? '暂无数值' : item.sample.value }} <span>{{ unitLabels[item.sample.unit] || item.sample.unit }}</span></p>
        <p>{{ semanticsLabels[item.sample.semantics] || item.sample.semantics }}<span v-if="item.sample.stage"> · {{ stageLabels[item.sample.stage] || item.sample.stage }}</span></p>
        <p v-if="item.sample.timeBasis === 'legacy-unzoned'">原始时间：{{ item.sample.start }}<span v-if="item.sample.semantics === 'interval'"> — {{ item.sample.end }}</span>（时区未确认）</p>
        <p v-else-if="item.sample.semantics === 'interval'">{{ item.sample.metadata?.timestampBasis === 'app-received' ? '应用观测区间' : '采样区间' }}：{{ localTime(item.sample.start) }} — {{ localTime(item.sample.end) }}</p>
        <p v-else>{{ item.sample.metadata?.timestampBasis === 'app-received' ? '应用接收时间' : '采样时间' }}：{{ localTime(item.sample.start) }}</p>
        <p class="latest-source">来源：{{ item.sample.source === 'yihe-harmonyos' ? '鸿蒙手表（应用上报）' : item.sample.source }} · {{ item.sample.sourceDevice }}<span v-if="item.deviceId != null"> · 绑定版本 {{ item.bindingVersion }}</span></p>
      </li>
    </ul>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, ref } from 'vue'
import { apiErrorMessage } from '../api/httpContract'
import { readLatestHealth, type LatestHealthRecord } from '../api/health'
import { createLatestRead } from '../composables/latestRead'
import { useSpeechReadout } from '../composables/useSpeechReadout'
import { buildDailyCareDigest } from '../domain/dailyCareDigest'

const items = shallowRef<LatestHealthRecord[]>([])
const loading = ref(false)
const error = ref('')
const { isSupported, isSpeaking, stop, toggle } = useSpeechReadout()
const nowMs = ref(Date.now())
const digest = computed(() => buildDailyCareDigest(items.value, nowMs.value))
let clock: ReturnType<typeof setInterval> | undefined

const reads = createLatestRead<void, LatestHealthRecord[]>(
  (_, signal) => readLatestHealth(signal),
  records => { items.value = records; error.value = '' },
  cause => { error.value = apiErrorMessage(cause, '健康采样暂时无法加载，请点击刷新采样重试。') },
  busy => { loading.value = busy }
)
const load = () => {
  stop()
  nowMs.value = Date.now()
  return reads.run()
}
onMounted(() => {
  void load()
  clock = setInterval(() => {
    if (!document.hidden) nowMs.value = Date.now()
  }, 60_000)
})
onBeforeUnmount(() => {
  if (clock !== undefined) clearInterval(clock)
  stop()
  reads.dispose()
})

const metricLabels: Record<string, string> = {
  heart_rate: '心率', blood_pressure_systolic: '收缩压', blood_pressure_diastolic: '舒张压',
  legacy_blood_pressure: '血压（旧协议）', oxygen_saturation: '血氧饱和度', body_temperature: '体温',
  body_mass: '体重', steps: '步数', sleep_duration: '睡眠时长', sleep_stage: '睡眠阶段'
}
const unitLabels: Record<string, string> = { 'count/min': '次/分', count: '步', min: '分钟', degC: '℃', kg: '千克', mmHg: 'mmHg', '%': '%' }
const semanticsLabels: Record<string, string> = { instant: '单次采样', daily_total: '日累计快照', interval: '区间采样' }
const stageLabels: Record<string, string> = { asleep: '睡眠', in_bed: '卧床', awake: '清醒', core: '浅睡', deep: '深睡', rem: '快速眼动' }
const localTime = (value: string) => new Date(value).toLocaleString('zh-CN', { timeZoneName: 'short' })
</script>

<style scoped>
.latest-health { margin: 0 0 28px; padding: 24px 28px; border: 1px solid var(--care-border); border-radius: var(--care-panel-radius); background: var(--care-surface); color: var(--care-heading); }
.latest-heading { display: flex; justify-content: space-between; align-items: start; gap: 16px; flex-wrap: wrap; }
.latest-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.latest-heading h2 { margin: 0; font-size: var(--care-section-title); font-weight: 600; }
.latest-heading p, .latest-empty { color: var(--care-text-muted); line-height: 1.7; max-width: 38em; }
.latest-heading p { margin: 6px 0 0; }
.latest-empty { margin: 16px 0 0; }
.digest-banner { margin-top: 14px; padding: 14px 16px; border-radius: var(--care-control-radius); background: var(--care-success-bg); }
.digest-banner--stale { background: var(--care-warning-bg); }
.digest-banner--unknown { background: var(--care-warning-bg); }
.digest-banner--empty { background: var(--care-neutral-bg); }
.digest-header { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: 8px; }
.digest-header strong { font-size: 17px; color: var(--care-heading); }
.digest-freshness { font-size: 14px; color: var(--care-text-muted); }
.digest-summary { margin: 6px 0 0; font-size: 15px; line-height: 1.75; color: var(--care-text-secondary); }
.latest-list { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 260px), 1fr)); gap: 16px; margin: 18px 0 0; padding: 0; list-style: none; }
.latest-list li { padding: 18px 20px; border: 1px solid var(--care-border-soft); border-radius: 12px; background: var(--care-surface-subtle); min-width: 0; }
.latest-list h3 { margin: 0; font-size: 17px; font-weight: 600; color: var(--care-text-secondary); }
.latest-list p { margin: 8px 0; line-height: 1.7; overflow-wrap: anywhere; }
.latest-list .latest-value { font-size: 28px; font-weight: 600; font-variant-numeric: tabular-nums; color: var(--care-primary); }
.latest-value span, .latest-source { font-size: 14px; color: var(--care-text-muted); }
:global(html.large-text) .digest-header strong { font-size: 20px; }
:global(html.large-text) .digest-summary { font-size: 18px; }
@media (max-width: 700px) { .latest-health { padding: 20px 18px; } }
</style>
