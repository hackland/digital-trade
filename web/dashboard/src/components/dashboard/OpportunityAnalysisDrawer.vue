<template>
  <el-drawer
    v-model="visible"
    :title="`市场机会分析 · ${symbol}`"
    direction="rtl"
    size="520px"
    :destroy-on-close="true"
    style="background: #1d1e1f"
  >
    <div v-if="loading" style="display:flex;align-items:center;justify-content:center;height:300px;color:#888">
      <el-icon class="is-loading" style="margin-right:8px"><Loading /></el-icon>分析中...
    </div>

    <div v-else-if="analysis" style="display:flex;flex-direction:column;gap:14px">

      <!-- 综合建议 -->
      <div :style="summaryStyle" style="padding:14px 16px;border-radius:8px;border-left:4px solid">
        <div style="font-size:15px;font-weight:600;margin-bottom:4px">
          {{ recommendLabel }}
        </div>
        <div style="font-size:13px;opacity:.85">{{ analysis.reason_summary }}</div>
      </div>

      <!-- 价格 + 评分 -->
      <el-card shadow="never" style="background:#252526;border-color:#333">
        <div style="display:grid;grid-template-columns:repeat(2,1fr);gap:12px;font-size:13px">
          <div>
            <div style="color:#888;margin-bottom:2px">当前价</div>
            <div style="color:#e0e0e0;font-weight:600">{{ fmt(analysis.current_price) }}</div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">综合评分</div>
            <div :style="{color: scoreReady ? '#67C23A' : '#e6a23c', fontWeight:600}">
              {{ analysis.composite_score.toFixed(4) }}
            </div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">买入阈值</div>
            <div style="color:#e0e0e0">{{ analysis.buy_threshold.toFixed(4) }}</div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">距阈值</div>
            <div :style="{color: analysis.score_gap <= 0 ? '#67C23A' : '#F56C6C'}">
              {{ analysis.score_gap > 0 ? '差 ' : '+' }}{{ Math.abs(analysis.score_gap).toFixed(4) }}
            </div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">日线 EMA50</div>
            <div style="color:#e6a23c">{{ fmt(analysis.daily_ema50) }}</div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">日线 EMA200</div>
            <div style="color:#909399">{{ fmt(analysis.daily_ema200) }}</div>
          </div>
        </div>
      </el-card>

      <!-- 维度 -->
      <el-card shadow="never" style="background:#252526;border-color:#333">
        <template #header><span style="color:#e0e0e0">分析维度</span></template>
        <div style="display:flex;flex-direction:column;gap:8px">
          <div
            v-for="dim in analysis.dimensions"
            :key="dim.name"
            style="display:flex;align-items:flex-start;gap:10px;padding:8px 10px;border-radius:6px;background:#1d1e1f"
          >
            <div style="width:8px;height:8px;border-radius:50%;margin-top:5px;flex-shrink:0"
              :style="{background: statusColor(dim.status)}" />
            <div style="flex:1;min-width:0">
              <div style="display:flex;justify-content:space-between;font-size:13px">
                <span style="color:#e0e0e0;font-weight:500">{{ dim.name }}</span>
                <span :style="{color: statusColor(dim.status)}">{{ dim.value }}</span>
              </div>
              <div style="font-size:12px;color:#888;margin-top:2px">{{ dim.detail }}</div>
            </div>
          </div>
        </div>
      </el-card>

      <div style="font-size:11px;color:#555;text-align:right">
        基于当前策略评分与市场状态，每次打开实时刷新
      </div>
    </div>

    <div v-else-if="error" style="display:flex;align-items:center;justify-content:center;height:200px;color:#888">
      {{ error }}
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import { get } from '@/api/http'

interface Dim { name: string; status: string; value: string; detail: string }
interface OpportunityAnalysis {
  symbol: string
  current_price: number
  daily_ema50: number; daily_ema200: number
  dist_to_ema50_pct: number; dist_to_ema200_pct: number
  composite_score: number; buy_threshold: number; score_gap: number
  regime: string; regime_label: string
  htf_bullish: boolean; htf_blocked: boolean
  cooldown_count: number; cooldown_bars: number
  dimensions: Dim[]
  level: string; recommendation: string; reason_summary: string
}

const props = defineProps<{ symbol: string }>()
const visible = defineModel<boolean>({ default: false })

const loading = ref(false)
const error = ref('')
const analysis = ref<OpportunityAnalysis | null>(null)

function fmt(v: number) {
  return v ? v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : '--'
}
function statusColor(s: string) {
  return s === 'ok' ? '#67C23A' : s === 'warning' ? '#e6a23c' : '#F56C6C'
}

const scoreReady = computed(() =>
  !!analysis.value && analysis.value.composite_score >= analysis.value.buy_threshold
)

const recommendLabel = computed(() => {
  const map: Record<string, string> = {
    strong_buy:   '🟢 明确买入机会',
    consider_buy: '🟡 可考虑小仓位入场',
    wait:         '⏳ 建议观望',
    avoid:        '🔴 建议规避',
  }
  return map[analysis.value?.recommendation ?? ''] ?? '--'
})

const summaryStyle = computed(() => {
  const lvl = analysis.value?.level
  const colors: Record<string, { bg: string; border: string }> = {
    good:    { bg: '#1a2a1a', border: '#67C23A' },
    neutral: { bg: '#1f2a35', border: '#909399' },
    caution: { bg: '#2a2510', border: '#e6a23c' },
    avoid:   { bg: '#3a1010', border: '#f56c6c' },
  }
  const c = colors[lvl ?? 'neutral']
  return { background: c.bg, borderColor: c.border, color: '#e0e0e0' }
})

async function load() {
  if (!props.symbol) return
  loading.value = true
  error.value = ''
  analysis.value = null
  try {
    analysis.value = await get<OpportunityAnalysis>(`/market/opportunity?symbol=${props.symbol}`)
  } catch (e: any) {
    error.value = e?.response?.data?.message || e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

watch(visible, (v) => { if (v) load() })
</script>
