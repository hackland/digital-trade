<template>
  <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
    <template #header>
      <div style="display: flex; align-items: center; justify-content: space-between">
        <span style="color: #e0e0e0">Positions</span>
        <!-- 全局：策略已暂停时显示恢复按钮 -->
        <el-button
          v-if="riskPaused"
          type="success"
          size="small"
          plain
          @click="handleResume"
        >
          ▶ 恢复策略
        </el-button>
      </div>
    </template>

    <el-table
      :data="positions"
      style="width: 100%"
      size="small"
      :header-cell-style="{ background: '#252526', color: '#b0b0b0' }"
      :cell-style="{ background: '#1d1e1f', color: '#e0e0e0' }"
    >
      <el-table-column prop="symbol" label="Symbol" width="110" />
      <el-table-column prop="side" label="Side" width="70">
        <template #default="{ row }">
          <el-tag
            :type="row.side === 'LONG' ? 'success' : row.side === 'SHORT' ? 'danger' : 'info'"
            size="small"
          >
            {{ row.side }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="quantity" label="Qty" width="90">
        <template #default="{ row }">{{ formatNumber(row.quantity, 6) }}</template>
      </el-table-column>
      <el-table-column prop="avg_entry_price" label="Entry">
        <template #default="{ row }">{{ formatPrice(row.avg_entry_price) }}</template>
      </el-table-column>
      <el-table-column prop="current_price" label="Current">
        <template #default="{ row }">{{ formatPrice(row.current_price) }}</template>
      </el-table-column>
      <el-table-column prop="unrealized_pnl" label="PnL">
        <template #default="{ row }">
          <span :style="{ color: row.unrealized_pnl >= 0 ? '#67C23A' : '#F56C6C' }">
            {{ formatPnl(row.unrealized_pnl) }}
          </span>
        </template>
      </el-table-column>

      <!-- 操作列 -->
      <el-table-column label="操作" width="220" fixed="right">
        <template #default="{ row }">
          <div v-if="row.side !== 'FLAT'" style="display: flex; gap: 4px; flex-wrap: wrap">
            <el-button
              size="small"
              type="primary"
              plain
              @click="openOverrideModal(row.symbol)"
            >
              + 条件触发
            </el-button>
            <el-button
              size="small"
              type="warning"
              plain
              @click="openPauseModal(row.symbol)"
            >
              暂停
            </el-button>
            <el-button
              size="small"
              type="info"
              plain
              @click="openAnalysis(row.symbol)"
            >
              分析
            </el-button>
            <el-button
              size="small"
              type="danger"
              plain
              @click="openForceCloseModal(row.symbol)"
            >
              立即平仓
            </el-button>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <!-- 活跃的条件触发列表 -->
    <div v-if="activeOverrides.length > 0" style="margin-top: 12px">
      <div style="color: #b0b0b0; font-size: 12px; margin-bottom: 6px">
        ◆ 条件触发 ({{ activeOverrides.length }} 个活跃)
      </div>
      <div
        v-for="ov in activeOverrides"
        :key="ov.id"
        style="
          display: flex;
          align-items: center;
          justify-content: space-between;
          padding: 6px 10px;
          background: #252526;
          border-radius: 4px;
          margin-bottom: 4px;
          font-size: 12px;
          color: #c0c0c0;
          border-left: 3px solid #409eff;
        "
      >
        <span>
          {{ ov.symbol }}
          <span style="color: #409eff">{{ ov.direction === 'above' ? '≥' : '≤' }}</span>
          <strong style="color: #e0e0e0">${{ ov.trigger_price.toLocaleString() }}</strong>
          →
          <span v-if="ov.actions.includes('force_close')" style="color: #f56c6c">平仓</span>
          <span
            v-if="ov.actions.includes('force_close') && ov.actions.includes('pause_strategy')"
          > + </span>
          <span v-if="ov.actions.includes('pause_strategy')" style="color: #e6a23c">
            暂停 {{ ov.pause_hours }}h
          </span>
          <span v-if="ov.note" style="color: #808080; margin-left: 6px">· {{ ov.note }}</span>
        </span>
        <el-button
          size="small"
          type="danger"
          text
          @click="handleCancelOverride(ov.id)"
        >
          取消
        </el-button>
      </div>
    </div>

    <el-empty v-if="positions.length === 0" description="No open positions" :image-size="60">
      <el-button type="primary" plain size="small" @click="openOpportunity">
        🔍 市场机会分析
      </el-button>
    </el-empty>

    <!-- 持仓分析抽屉 -->
    <PositionAnalysisDrawer v-model="analysisVisible" :symbol="analysisSymbol" />

    <!-- 市场机会分析抽屉（无持仓时） -->
    <OpportunityAnalysisDrawer v-model="opportunityVisible" :symbol="opportunitySymbol" />

    <!-- 弹窗（每个 symbol 共用一个，通过 ref 控制） -->
    <OverrideModal
      v-if="currentSymbol"
      :ref="(el: any) => (modalRef = el)"
      :symbol="currentSymbol"
      @created="onOverrideCreated"
      @force-closed="emit('refresh')"
      @paused="emit('refresh')"
    />
  </el-card>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Position, ConditionalOverride } from '@/types/models'
import { formatNumber, formatPrice, formatPnl } from '@/utils/format'
import { fetchOverrides, cancelOverride, resumeStrategy } from '@/api/override'
import OverrideModal from './OverrideModal.vue'
import PositionAnalysisDrawer from './PositionAnalysisDrawer.vue'
import OpportunityAnalysisDrawer from './OpportunityAnalysisDrawer.vue'

const props = defineProps<{
  positions: Position[]
  riskPaused?: boolean
}>()

const emit = defineEmits<{
  (e: 'refresh'): void
}>()

// --- override 列表 ---
const allOverrides = ref<ConditionalOverride[]>([])
const activeOverrides = computed(() =>
  allOverrides.value.filter((o) => o.status === 'active')
)

async function loadOverrides() {
  try {
    allOverrides.value = await fetchOverrides()
  } catch {}
}

onMounted(loadOverrides)

function onOverrideCreated() {
  loadOverrides()
}

async function handleCancelOverride(id: string) {
  try {
    await cancelOverride(id)
    ElMessage.success('条件触发已取消')
    loadOverrides()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '取消失败')
  }
}

// --- 弹窗控制 ---
const currentSymbol = ref('')
const modalRef = ref<InstanceType<typeof OverrideModal> | null>(null)

function openOverrideModal(symbol: string) {
  currentSymbol.value = symbol
  // nextTick 等 DOM 更新后再打开
  setTimeout(() => modalRef.value?.open(), 50)
}

function openForceCloseModal(symbol: string) {
  currentSymbol.value = symbol
  setTimeout(() => modalRef.value?.openForceClose(), 50)
}

function openPauseModal(symbol: string) {
  currentSymbol.value = symbol
  setTimeout(() => modalRef.value?.openPause(), 50)
}

// --- 持仓分析 ---
const analysisVisible = ref(false)
const analysisSymbol = ref('')

function openAnalysis(symbol: string) {
  analysisSymbol.value = symbol
  analysisVisible.value = true
}

// --- 市场机会分析（无持仓时） ---
const opportunityVisible = ref(false)
const opportunitySymbol = ref('BTCUSDT')

function openOpportunity() {
  opportunitySymbol.value = 'BTCUSDT'
  opportunityVisible.value = true
}

// --- 恢复策略 ---
async function handleResume() {
  try {
    await resumeStrategy()
    ElMessage.success('策略已恢复')
    emit('refresh')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '恢复失败')
  }
}
</script>
