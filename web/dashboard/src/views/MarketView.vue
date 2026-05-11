<template>
  <div>
    <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
      <div style="display: flex; gap: 16px; align-items: center; justify-content: space-between">
        <!-- 左：交易对 + 周期 -->
        <div style="display: flex; gap: 16px; align-items: center">
          <el-select v-model="symbol" style="width: 160px" size="default">
            <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
          </el-select>
          <el-radio-group v-model="interval" size="small">
            <el-radio-button v-for="i in INTERVALS" :key="i" :value="i">{{ i }}</el-radio-button>
          </el-radio-group>
        </div>

        <!-- 右：市场状态 -->
        <div style="display: flex; align-items: center; gap: 10px">
          <span v-if="regimeLoading" style="font-size: 12px; color: #888">检测中...</span>
          <template v-else-if="regime">
            <el-tooltip placement="bottom" :hide-after="0">
              <template #content>
                <div style="font-size: 12px; line-height: 1.8">
                  <div>日线 EMA50：{{ fmt(regime.daily_ema50) }}</div>
                  <div>日线 EMA200：{{ fmt(regime.daily_ema200) }}</div>
                  <div>周线 EMA200：{{ fmt(regime.weekly_ema200) }}</div>
                  <div style="margin-top: 4px; color: #aaa">
                    日线：价格 {{ regime.daily_bull ? '>' : '<' }} EMA200（{{ regime.daily_bull ? '偏多' : '偏空' }}）
                    &nbsp;|&nbsp;
                    周线：EMA200 {{ regime.weekly_bull ? '向上 ↑（宏观牛）' : '向下 ↓（宏观熊）' }}
                  </div>
                </div>
              </template>
              <el-tag
                :color="regimeColor"
                style="cursor: default; font-weight: 600; border: none; font-size: 13px; padding: 0 12px; height: 28px; line-height: 28px"
              >
                {{ regime.regime_label }}
              </el-tag>
            </el-tooltip>
          </template>
          <span v-else style="font-size: 12px; color: #888">--</span>
        </div>
      </div>
    </el-card>

    <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
      <KlineChart :symbol="symbol" :interval="interval" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import KlineChart from '@/components/charts/KlineChart.vue'
import { SYMBOLS, INTERVALS } from '@/utils/constants'
import { get } from '@/api/http'

interface RegimeResult {
  symbol: string
  price: number
  daily_ema50: number
  daily_ema200: number
  daily_bull: boolean
  weekly_ema200: number
  weekly_ema200_prev: number
  weekly_bull: boolean
  regime: string
  regime_label: string
}

const symbol = ref('BTCUSDT')
const interval = ref('5m')

const regime = ref<RegimeResult | null>(null)
const regimeLoading = ref(false)

const regimeColor = computed(() => {
  switch (regime.value?.regime) {
    case 'strong_bull':  return '#1a3a1a'  // 深绿
    case 'bear_bounce':  return '#3a2a1a'  // 橙褐（短期反弹，宏观偏空）
    case 'mid_bear':     return '#3a2210'  // 深橙（中期熊，长线未崩）
    case 'strong_bear':  return '#3a1a1a'  // 深红
    default:             return '#2a2a2a'
  }
})

function fmt(v: number) {
  return v ? v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : '--'
}

async function loadRegime() {
  regimeLoading.value = true
  try {
    regime.value = await get<RegimeResult>('/market/regime', { symbol: symbol.value })
  } catch {
    regime.value = null
  } finally {
    regimeLoading.value = false
  }
}

watch(symbol, loadRegime)
onMounted(loadRegime)
</script>
