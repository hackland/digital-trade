<template>
  <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
    <template #header>
      <div style="display: flex; justify-content: space-between; align-items: center; color: #e0e0e0">
        <span>Trade History</span>
        <div style="display: flex; gap: 8px">
          <el-select v-model="filter.symbol" placeholder="Symbol" clearable size="small" style="width: 140px">
            <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
          </el-select>
          <el-button size="small" @click="loadData">Search</el-button>
        </div>
      </div>
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
      <el-table-column prop="fee" label="Fee">
        <template #default="{ row }">{{ formatNumber(row.fee, 6) }} {{ row.fee_asset }}</template>
      </el-table-column>
      <el-table-column prop="realized_pnl" label="PnL">
        <template #default="{ row }">
          <span :style="{ color: row.realized_pnl >= 0 ? '#67C23A' : '#F56C6C' }">
            {{ formatPnl(row.realized_pnl) }}
          </span>
        </template>
      </el-table-column>
      <el-table-column prop="strategy_name" label="Strategy" />
      <el-table-column prop="timestamp" label="Time" width="160">
        <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
      </el-table-column>
    </el-table>
    <div style="margin-top: 16px; display: flex; justify-content: center">
      <el-pagination :current-page="page" :page-size="pageSize" :total="total" layout="prev, pager, next" @current-change="onPageChange" />
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { fetchTrades } from '@/api/trades'
import type { TradeRecord } from '@/types/models'
import { SYMBOLS } from '@/utils/constants'
import { formatNumber, formatPrice, formatPnl, formatTime } from '@/utils/format'

const trades = ref<TradeRecord[]>([])
const filter = ref({ symbol: '' })
const page = ref(1)
const pageSize = 20
const total = ref(0)

async function loadData() {
  try {
    const params: Record<string, any> = { limit: pageSize, offset: (page.value - 1) * pageSize }
    if (filter.value.symbol) params.symbol = filter.value.symbol
    const res = await fetchTrades(params)
    trades.value = res.data as TradeRecord[]
    total.value = res.total
  } catch {}
}

function onPageChange(p: number) {
  page.value = p
  loadData()
}

onMounted(loadData)
</script>
