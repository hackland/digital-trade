<template>
  <el-table
    :data="rows" size="small" :empty-text="empty" max-height="420" highlight-current-row
    :header-cell-style="{ background: '#232425', color: '#b0b0b0' }" @row-click="(r: IctSetup) => emit('select', r)"
  >
    <el-table-column label="信号时间" width="120">
      <template #default="{ row }">{{ fmt(row.arm_time) }}</template>
    </el-table-column>
    <el-table-column label="方向" width="70">
      <template #default="{ row }">
        <el-tag :type="row.dir === 'long' ? 'success' : 'danger'" size="small">{{ row.dir === 'long' ? '做多' : '做空' }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column label="状态" width="90">
      <template #default="{ row }">
        <el-tag :type="statusType(row.status)" size="small" effect="plain">{{ statusText[row.status as IctStatus] }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column label="触发" width="140">
      <template #default="{ row }">
        <template v-if="row.model === 'breakout'">顺势突破 @ {{ row.mss_level.toFixed(0) }}</template>
        <template v-else>扫荡 {{ row.sweep_kind }} @ {{ row.sweep_level.toFixed(0) }}</template>
      </template>
    </el-table-column>
    <el-table-column label="入场区域" width="170">
      <template #default="{ row }">{{ row.zone_kind }} {{ row.zone_bottom.toFixed(0) }}–{{ row.zone_top.toFixed(0) }}</template>
    </el-table-column>
    <el-table-column label="入场" width="90"><template #default="{ row }">{{ row.entry.toFixed(0) }}</template></el-table-column>
    <el-table-column label="止损" width="90"><template #default="{ row }">{{ row.stop.toFixed(0) }}</template></el-table-column>
    <el-table-column label="止盈" width="130">
      <template #default="{ row }">{{ row.tp.toFixed(0) }}（{{ row.planned_rr.toFixed(1) }}R·{{ row.tp_kind }}）</template>
    </el-table-column>
    <el-table-column label="4h偏向" width="80">
      <template #default="{ row }">{{ biasText[row.bias as 'up' | 'down' | 'none'] }}</template>
    </el-table-column>
    <el-table-column label="结果R" width="90">
      <template #default="{ row }">
        <b v-if="['tp', 'sl', 'timeout', 'open'].includes(row.status)" :class="row.r >= 0 ? 'win' : 'loss'">
          {{ row.r >= 0 ? '+' : '' }}{{ row.r.toFixed(2) }}
        </b>
        <span v-else class="muted">-</span>
      </template>
    </el-table-column>
  </el-table>
</template>

<script setup lang="ts">
import type { IctSetup, IctStatus } from '@/api/ict'

defineProps<{ rows: IctSetup[]; empty?: string }>()
const emit = defineEmits<{ (e: 'select', row: IctSetup): void }>()

const statusText: Record<IctStatus, string> = {
  pending: '等待回踩', open: '持仓中', tp: '止盈', sl: '止损', timeout: '超时平仓',
  expired: '未成交', missed: '错过', invalid: '跳空失效',
}
const biasText = { up: '偏多', down: '偏空', none: '未定' }

function statusType(s: IctStatus) {
  if (s === 'tp') return 'success'
  if (s === 'sl') return 'danger'
  if (s === 'pending' || s === 'open') return 'warning'
  return 'info'
}

function fmt(iso: string) {
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
</script>

<style scoped>
.win { color: #26a69a }
.loss { color: #ef5350 }
.muted { color: #888 }
</style>
