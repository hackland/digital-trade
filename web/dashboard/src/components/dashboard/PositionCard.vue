<template>
  <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
    <template #header>
      <span style="color: #e0e0e0">Positions</span>
    </template>
    <el-table :data="positions" style="width: 100%" size="small" :header-cell-style="{ background: '#252526', color: '#b0b0b0' }" :cell-style="{ background: '#1d1e1f', color: '#e0e0e0' }">
      <el-table-column prop="symbol" label="Symbol" width="120" />
      <el-table-column prop="side" label="Side" width="80">
        <template #default="{ row }">
          <el-tag :type="row.side === 'LONG' ? 'success' : row.side === 'SHORT' ? 'danger' : 'info'" size="small">
            {{ row.side }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="quantity" label="Qty" width="100">
        <template #default="{ row }">{{ formatNumber(row.quantity, 6) }}</template>
      </el-table-column>
      <el-table-column prop="avg_entry_price" label="Entry Price">
        <template #default="{ row }">{{ formatPrice(row.avg_entry_price) }}</template>
      </el-table-column>
      <el-table-column prop="current_price" label="Current">
        <template #default="{ row }">{{ formatPrice(row.current_price) }}</template>
      </el-table-column>
      <el-table-column prop="unrealized_pnl" label="Unrealized PnL">
        <template #default="{ row }">
          <span :style="{ color: row.unrealized_pnl >= 0 ? '#67C23A' : '#F56C6C' }">
            {{ formatPnl(row.unrealized_pnl) }}
          </span>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="positions.length === 0" description="No open positions" :image-size="60" />
  </el-card>
</template>

<script setup lang="ts">
import type { Position } from '@/types/models'
import { formatNumber, formatPrice, formatPnl } from '@/utils/format'

defineProps<{ positions: Position[] }>()
</script>
