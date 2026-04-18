<template>
  <div>
    <!-- Config Panel -->
    <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
      <template #header>
        <div style="display: flex; justify-content: space-between; align-items: center">
          <span style="color: #f0b90b; font-weight: 600">
            做空策略配置
            <el-tag type="warning" size="small" style="margin-left: 8px">仅告警</el-tag>
          </span>
          <div style="display: flex; gap: 8px">
            <el-popconfirm
              title="确认将做空策略参数部署到实盘引擎？(仅告警，不自动交易)"
              confirm-button-text="确认部署"
              cancel-button-text="取消"
              @confirm="deployHandler"
            >
              <template #reference>
                <el-button type="warning" size="small" :loading="deploying">
                  Deploy to Live
                </el-button>
              </template>
            </el-popconfirm>
          </div>
        </div>
      </template>

      <el-form :inline="true" size="small" label-position="top">
        <el-form-item label="Symbol">
          <el-select v-model="form.symbol" style="width: 130px">
            <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
          </el-select>
        </el-form-item>
        <el-form-item label="Interval">
          <el-select v-model="form.interval" style="width: 100px">
            <el-option v-for="i in INTERVALS" :key="i" :label="i" :value="i" />
          </el-select>
        </el-form-item>
        <el-form-item label="Initial Cash">
          <el-input-number v-model="form.cash" :min="100" :step="1000" style="width: 140px" />
        </el-form-item>
        <el-form-item label="Alloc %">
          <el-input-number v-model="allocPct" :min="1" :max="100" :step="5" style="width: 100px" />
        </el-form-item>
        <el-form-item label="Fee %">
          <el-input-number v-model="feePct" :min="0" :max="1" :step="0.01" :precision="3" style="width: 110px" />
        </el-form-item>
      </el-form>

      <div class="time-range-row">
        <div class="quick-btns">
          <el-button
            v-for="preset in dayPresets"
            :key="preset.days"
            :type="activeDays === preset.days ? 'primary' : 'default'"
            size="small"
            @click="selectPreset(preset.days)"
            class="quick-btn"
            :style="activeDays === preset.days
              ? 'background: #f0b90b; border-color: #f0b90b; color: #000; font-size: 12px; font-weight: 600'
              : 'background: #252526; border-color: #444; color: #b0b0b0; font-size: 12px; font-weight: 600'"
          >
            {{ preset.label }}
          </el-button>
        </div>

        <el-button
          type="primary"
          size="small"
          @click="runBacktestHandler"
          :loading="loading"
          style="background: #f0b90b; border-color: #f0b90b; color: #000; margin-left: 16px; height: 32px; font-weight: 600"
        >
          Run Backtest
        </el-button>
      </div>

      <!-- Module Selection (same as BacktestView) -->
      <div class="cw-panel">
        <div class="cw-section" v-for="cat in categoryOrder" :key="cat">
          <div class="cw-category-title">{{ categoryLabels[cat] || cat }}</div>
          <div v-for="mod in groupedModules[cat]" :key="mod.name" class="cw-module-row">
            <el-checkbox
              v-model="enabledModules[mod.name]"
              class="cw-checkbox"
            >
              <span class="cw-mod-label">{{ mod.label }}</span>
            </el-checkbox>
            <el-tooltip :content="mod.description" placement="top">
              <el-icon class="cw-info-icon"><QuestionFilled /></el-icon>
            </el-tooltip>
            <div class="cw-weight-area" v-if="enabledModules[mod.name]">
              <el-slider
                v-model="moduleWeights[mod.name]"
                :min="0" :max="100" :step="5"
                :show-tooltip="false"
                style="width: 120px"
                size="small"
              />
              <span class="cw-weight-val">{{ moduleWeights[mod.name] }}%</span>
            </div>
            <div class="cw-params" v-if="enabledModules[mod.name] && mod.params?.length">
              <span v-for="p in mod.params" :key="p.key" class="cw-param-item">
                <span class="cw-param-label">{{ p.label }}:</span>
                <el-input-number
                  v-model="moduleParams[mod.name][p.key]"
                  :min="p.min" :max="p.max" :step="p.step"
                  size="small"
                  controls-position="right"
                  style="width: 90px"
                />
              </span>
            </div>
          </div>
        </div>

        <!-- Weight summary -->
        <div class="cw-weight-summary">
          <span>权重合计: </span>
          <span :style="{ color: totalWeight === 100 ? '#67C23A' : '#F56C6C', fontWeight: 600 }">
            {{ totalWeight }}%
          </span>
          <span v-if="totalWeight !== 100" style="color: #F56C6C; margin-left: 8px; font-size: 12px">
            (建议调整至 100%)
          </span>
          <el-button size="small" text type="info" @click="resetModules" style="margin-left: 12px">重置默认</el-button>
        </div>

        <!-- Signal controls - grouped -->
        <div class="cw-section" style="margin-top: 12px">
          <div class="cw-category-title">做空信号控制</div>
          <div class="cw-signal-groups">
            <div v-for="grp in signalGroups" :key="grp.key" class="cw-signal-group">
              <div class="cw-group-header">{{ grp.label }}</div>
              <div class="cw-group-params">
                <div v-for="sp in getParamsByGroup(grp.key)" :key="sp.key" class="cw-signal-param">
                  <div class="cw-signal-param-top">
                    <span class="cw-signal-param-label">{{ sp.label }}</span>
                    <el-tooltip v-if="sp.desc" :content="sp.desc" placement="top" :show-after="200">
                      <el-icon class="cw-info-icon"><QuestionFilled /></el-icon>
                    </el-tooltip>
                  </div>
                  <el-switch
                    v-if="sp.type === 'bool'"
                    v-model="signalConfig[sp.key]"
                    size="small"
                  />
                  <el-select
                    v-else-if="sp.type === 'string'"
                    v-model="signalConfig[sp.key]"
                    size="small"
                    style="width: 80px"
                  >
                    <el-option label="1h" value="1h" />
                    <el-option label="4h" value="4h" />
                    <el-option label="1d" value="1d" />
                  </el-select>
                  <el-input-number
                    v-else
                    v-model="signalConfig[sp.key]"
                    :min="sp.min" :max="sp.max" :step="sp.step"
                    :precision="sp.type === 'float' ? 2 : 0"
                    size="small"
                    controls-position="right"
                    style="width: 100px"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Live status -->
      <div v-if="liveConfig" class="live-status">
        <el-tag :type="liveConfig.short_enabled ? 'success' : 'info'" size="small">
          {{ liveConfig.short_enabled ? 'Live: ON' : 'Live: OFF' }}
        </el-tag>
        <span v-if="liveConfig.short_enabled" style="color: #b0b0b0; font-size: 12px; margin-left: 8px">
          threshold={{ liveConfig.short_threshold }}, cover={{ liveConfig.cover_threshold }}, min_hold={{ liveConfig.short_min_hold_bars }}
        </span>
      </div>
    </el-card>

    <!-- Results -->
    <template v-if="result">
      <el-row :gutter="16" style="margin-bottom: 16px">
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Short Return</div>
              <div class="metric-value" :style="{ color: result.short_metrics.total_return >= 0 ? '#67C23A' : '#F56C6C' }">
                {{ result.short_metrics.total_return >= 0 ? '+' : '' }}{{ result.short_metrics.total_return.toFixed(2) }} USDT
              </div>
              <div class="metric-sub">
                {{ result.short_metrics.total_return_pct >= 0 ? '+' : '' }}{{ result.short_metrics.total_return_pct.toFixed(2) }}%
                · {{ result.short_metrics.total_trades }} trades
              </div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Win Rate</div>
              <div class="metric-value" style="color: #f0b90b">{{ (result.short_metrics.win_rate * 100).toFixed(1) }}%</div>
              <div class="metric-sub">{{ result.short_metrics.win_trades }}W / {{ result.short_metrics.lose_trades }}L</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Avg Win / Loss</div>
              <div class="metric-value" style="color: #67C23A">${{ result.short_metrics.avg_win.toFixed(2) }}</div>
              <div class="metric-sub" style="color: #F56C6C">Loss: ${{ result.short_metrics.avg_loss.toFixed(2) }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Profit Factor</div>
              <div class="metric-value" style="color: #e0e0e0">{{ result.short_metrics.profit_factor.toFixed(2) }}</div>
              <div class="metric-sub">Fees: ${{ result.short_metrics.total_fees.toFixed(2) }}</div>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <!-- Long vs Short comparison -->
      <el-card shadow="never" style="background: #252526; border-color: #333; margin-bottom: 16px">
        <div style="color: #888; font-size: 12px; margin-bottom: 4px">Long vs Short comparison (same period, same modules)</div>
        <span style="color: #e0e0e0; font-size: 13px">
          Long: {{ result.metrics.total_return >= 0 ? '+' : '' }}{{ result.metrics.total_return.toFixed(2) }} USDT
          ({{ result.metrics.total_return_pct.toFixed(2) }}%, {{ result.metrics.total_trades }} trades, WR={{ (result.metrics.win_rate*100).toFixed(1) }}%)
          &nbsp;|&nbsp;
          Short: {{ result.short_metrics.total_return >= 0 ? '+' : '' }}{{ result.short_metrics.total_return.toFixed(2) }} USDT
          ({{ result.short_metrics.total_return_pct.toFixed(2) }}%, {{ result.short_metrics.total_trades }} trades, WR={{ (result.short_metrics.win_rate*100).toFixed(1) }}%)
        </span>
      </el-card>

      <!-- Trade list -->
      <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
        <template #header>
          <span style="color: #e0e0e0">Short Trade History ({{ result.short_trades?.length || 0 }})</span>
        </template>
        <el-table
          :data="result.short_trades"
          style="width: 100%"
          size="small"
          max-height="500"
          :header-cell-style="{ background: '#252526', color: '#b0b0b0' }"
          :cell-style="() => ({ background: '#1d1e1f', color: '#e0e0e0' })"
        >
          <el-table-column label="Time" width="160">
            <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
          </el-table-column>
          <el-table-column prop="side" label="Side" width="80">
            <template #default="{ row }">
              <el-tag :type="row.side === 'SHORT' ? 'warning' : 'info'" size="small">{{ row.side }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="Price" width="100">
            <template #default="{ row }">{{ formatPrice(row.price) }}</template>
          </el-table-column>
          <el-table-column label="Qty" width="100">
            <template #default="{ row }">{{ row.quantity.toFixed(6) }}</template>
          </el-table-column>
          <el-table-column label="Fee (U)" width="80">
            <template #default="{ row }">{{ row.fee.toFixed(2) }}</template>
          </el-table-column>
          <el-table-column label="PnL (U)" width="100">
            <template #default="{ row }">
              <span v-if="row.side === 'COVER'" :style="{ color: row.pnl >= 0 ? '#67C23A' : '#F56C6C' }">
                {{ row.pnl >= 0 ? '+' : '' }}{{ row.pnl.toFixed(2) }}
              </span>
              <span v-else style="color: #888">-</span>
            </template>
          </el-table-column>
          <el-table-column label="Reason" min-width="200" show-overflow-tooltip>
            <template #default="{ row }">{{ row.reason }}</template>
          </el-table-column>
        </el-table>
      </el-card>
    </template>

    <!-- Empty state -->
    <el-card v-else-if="!loading" shadow="never" style="background: #1d1e1f; border-color: #333; text-align: center; padding: 60px 0">
      <el-empty description="Configure modules & short params, then run backtest" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { runBacktest as apiRunBacktest, getIndicatorModules, deployStrategy, type BacktestResult, type ParamSchema, type ModuleMeta } from '@/api/backtest'
import http from '@/api/http'
import { SYMBOLS, INTERVALS } from '@/utils/constants'
import { formatPrice, formatTime } from '@/utils/format'
import { ElMessage } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'

const loading = ref(false)
const deploying = ref(false)
const result = ref<BacktestResult | null>(null)
const liveConfig = ref<Record<string, any> | null>(null)
const feePct = ref(0.075)
const allocPct = ref(100)

const form = reactive({
  symbol: 'BTCUSDT',
  interval: '1h',
  days: 365,
  cash: 10000,
})

const dayPresets = [
  { label: '7D', days: 7 },
  { label: '30D', days: 30 },
  { label: '90D', days: 90 },
  { label: '180D', days: 180 },
  { label: '365D', days: 365 },
  { label: '2Y', days: 730 },
]

// preset-only：用于渲染 active 态并驱动 days 参数
const activeDays = ref(form.days)

function selectPreset(days: number) {
  activeDays.value = days
  form.days = days
}

// --- Module config (same as BacktestView) ---
const categoryOrder = ['trend', 'momentum', 'money_flow', 'volume']
const categoryLabels: Record<string, string> = {
  trend: '趋势类',
  momentum: '动量类',
  money_flow: '资金流类',
  volume: '成交量类',
}

const groupedModules = ref<Record<string, ModuleMeta[]>>({})
const allSignalParams = ref<ParamSchema[]>([])
const enabledModules = ref<Record<string, boolean>>({})
const moduleWeights = ref<Record<string, number>>({})
const moduleParams = ref<Record<string, Record<string, any>>>({})
const signalConfig = ref<Record<string, any>>({})
let allModules: ModuleMeta[] = []

// Show short params + trend filter params (since trend filter affects short entry)
const signalGroups = [
  { key: 'short', label: '做空信号' },
  { key: 'trend', label: '趋势过滤 (做空需要趋势向下)' },
]

function getParamsByGroup(group: string): ParamSchema[] {
  return allSignalParams.value.filter(sp => sp.group === group)
}

const totalWeight = computed(() => {
  let sum = 0
  for (const [name, enabled] of Object.entries(enabledModules.value)) {
    if (enabled) sum += (moduleWeights.value[name] || 0)
  }
  return sum
})

function resetModules() {
  enabledModules.value = {}
  moduleWeights.value = {}
  moduleParams.value = {}

  // Default: backtest-tuned weights for short strategy
  const defaultMods: Record<string, number> = {
    'ema_cross': 20,
    'macd': 60,
    'mfi': 20,
  }
  for (const mod of allModules) {
    enabledModules.value[mod.name] = mod.name in defaultMods
    moduleWeights.value[mod.name] = defaultMods[mod.name] ?? Math.round(mod.default_weight * 100)
    const params: Record<string, any> = {}
    for (const p of (mod.params || [])) {
      params[p.key] = p.default
    }
    moduleParams.value[mod.name] = params
  }
}

function buildStrategyConfig(): Record<string, any> {
  const mods: any[] = []
  for (const [name, enabled] of Object.entries(enabledModules.value)) {
    if (!enabled) continue
    const weight = (moduleWeights.value[name] || 0) / 100
    if (weight <= 0) continue
    mods.push({
      name,
      weight,
      params: moduleParams.value[name] || {},
    })
  }
  return {
    modules: mods,
    ...signalConfig.value,
    short_enabled: true,
  }
}

onMounted(async () => {
  try {
    const data = await getIndicatorModules()
    allModules = data.modules
    groupedModules.value = data.grouped
    allSignalParams.value = data.signal_params || []
    // Init signal config defaults
    for (const sp of allSignalParams.value) {
      signalConfig.value[sp.key] = sp.default
    }
    // Force short enabled
    signalConfig.value.short_enabled = true
  } catch (e: any) {
    ElMessage.error('Failed to load modules: ' + e.message)
  }
  resetModules()

  // Load live strategy status
  try {
    const res = await http.get('/strategy/status')
    liveConfig.value = (res.data as any).data?.config
  } catch {}
})

async function runBacktestHandler() {
  loading.value = true
  result.value = null
  try {
    const strategyConfig = buildStrategyConfig()
    const res = await apiRunBacktest({
      symbol: form.symbol,
      interval: form.interval,
      strategy: 'custom_weighted',
      strategy_config: strategyConfig,
      days: form.days,
      cash: form.cash,
      fee: feePct.value / 100,
      alloc: allocPct.value / 100,
    })
    result.value = res
    if (!res.short_trades?.length) {
      ElMessage.warning('No short trades generated. Try adjusting thresholds or disabling trend filter.')
    } else {
      ElMessage.success(`Short backtest: ${res.short_metrics.total_trades} trades, ${res.short_metrics.total_return_pct.toFixed(2)}% return`)
    }
  } catch (e: any) {
    ElMessage.error('Backtest failed: ' + (e.response?.data?.message || e.message))
  } finally {
    loading.value = false
  }
}

async function deployHandler() {
  deploying.value = true
  try {
    const config = buildStrategyConfig()
    const mods = config.modules.map((m: any) => ({ name: m.name, weight: m.weight }))
    const params: Record<string, any> = { ...config }
    delete params.modules

    await deployStrategy({ modules: mods, signal_params: params })
    ElMessage.success('做空策略已部署 (仅告警模式)')

    const res = await http.get('/strategy/status')
    liveConfig.value = (res.data as any).data?.config
  } catch (e: any) {
    ElMessage.error('Deploy failed: ' + (e.response?.data?.message || e.message))
  } finally {
    deploying.value = false
  }
}
</script>

<style scoped>
.cw-panel {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid #333;
}
.cw-section { margin-bottom: 12px; }
.time-range-row {
  display: flex;
  align-items: center;
  margin-top: 8px;
  padding-top: 12px;
  border-top: 1px solid #333;
}
.quick-btns {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.quick-btn :deep(.el-button__content) { font-weight: 600; }
.cw-category-title {
  color: #f0b90b;
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 6px;
  padding-left: 2px;
}
.cw-module-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0 4px 8px;
  flex-wrap: wrap;
}
.cw-checkbox :deep(.el-checkbox__label) {
  color: #e0e0e0 !important;
  font-size: 13px;
}
.cw-mod-label { min-width: 100px; display: inline-block; }
.cw-info-icon { color: #666; font-size: 14px; cursor: help; }
.cw-weight-area {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: 4px;
}
.cw-weight-val {
  color: #f0b90b;
  font-size: 12px;
  min-width: 32px;
  text-align: right;
}
.cw-params { display: flex; gap: 10px; margin-left: 12px; }
.cw-param-item { display: flex; align-items: center; gap: 4px; }
.cw-param-label { color: #888; font-size: 12px; white-space: nowrap; }
.cw-weight-summary {
  display: flex;
  align-items: center;
  padding: 8px 8px 0 8px;
  margin-top: 4px;
  border-top: 1px solid #333;
  color: #b0b0b0;
  font-size: 13px;
}
.cw-signal-groups {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 12px;
  padding: 4px 0;
}
.cw-signal-group {
  background: #252526;
  border: 1px solid #333;
  border-radius: 6px;
  padding: 10px 12px;
}
.cw-group-header {
  color: #b0b0b0;
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 8px;
  padding-bottom: 4px;
  border-bottom: 1px solid #333;
}
.cw-group-params { display: flex; flex-direction: column; gap: 8px; }
.cw-signal-param {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.cw-signal-param-top { display: flex; align-items: center; gap: 4px; }
.cw-signal-param-label { color: #e0e0e0; font-size: 13px; white-space: nowrap; }

.live-status {
  margin-top: 12px;
  padding: 8px 12px;
  background: #252526;
  border-radius: 4px;
}
.metric-card { text-align: center; }
.metric-label { color: #888; font-size: 12px; margin-bottom: 4px; }
.metric-value { font-size: 20px; font-weight: bold; }
.metric-sub { color: #888; font-size: 12px; margin-top: 2px; }

:deep(.el-form-item__label) { color: #b0b0b0 !important; font-size: 12px !important; }
:deep(.el-input__inner),
:deep(.el-input-number__decrease),
:deep(.el-input-number__increase) {
  background: #252526 !important;
  color: #e0e0e0 !important;
  border-color: #444 !important;
}
:deep(.el-select .el-input__inner) { background: #252526 !important; color: #e0e0e0 !important; }
:deep(.el-empty__description p) { color: #888 !important; }
:deep(.cw-module-row .el-slider__runway) { background-color: #333; }
:deep(.cw-module-row .el-slider__bar) { background-color: #f0b90b; }
:deep(.cw-module-row .el-slider__button) { border-color: #f0b90b; width: 12px; height: 12px; }
</style>
