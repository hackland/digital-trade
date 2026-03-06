<template>
  <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
    <template #header>
      <span style="color: #e0e0e0">Recent Trades</span>
    </template>
    <el-table :data="trades" style="width: 100%" size="small" :header-cell-style="{ background: '#252526', color: '#b0b0b0' }" :cell-style="{ background: '#1d1e1f', color: '#e0e0e0' }">
      <el-table-column prop="symbol" label="Symbol" width="100" />
      <el-table-column prop="side" label="Side" width="70">
        <template #default="{ row }">
          <el-tag :type="row.side === 'BUY' ? 'success' : 'danger'" size="small">{{ row.side }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="price" label="Price">
        <template #default="{ row }">{{ formatPrice(row.price) }}</template>
      </el-table-column>
      <el-table-column prop="quantity" label="Qty">
        <template #default="{ row }">{{ formatNumber(row.quantity, 6) }}</template>
      </el-table-column>
      <el-table-column prop="realized_pnl" label="PnL">
        <template #default="{ row }">
          <span :style="{ color: row.realized_pnl >= 0 ? '#67C23A' : '#F56C6C' }">
            {{ formatPnl(row.realized_pnl) }}
          </span>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="trades.length === 0" description="No recent trades" :image-size="60" />
  </el-card>
</template>

<script setup lang="ts">
import type { TradeRecord } from '@/types/models'
import { formatNumber, formatPrice, formatPnl } from '@/utils/format'

defineProps<{ trades: TradeRecord[] }>()
</script>
