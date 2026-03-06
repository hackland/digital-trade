<template>
  <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
    <template #header>
      <div style="display: flex; justify-content: space-between; align-items: center; color: #e0e0e0">
        <span>Signal History</span>
        <div style="display: flex; gap: 8px">
          <el-select v-model="filter.symbol" placeholder="Symbol" clearable size="small" style="width: 140px">
            <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
          </el-select>
          <el-select v-model="filter.action" placeholder="Action" clearable size="small" style="width: 100px">
            <el-option label="BUY" value="BUY" />
            <el-option label="SELL" value="SELL" />
          </el-select>
          <el-button size="small" @click="loadData">Search</el-button>
        </div>
      </div>
    </template>
    <el-table :data="signals" style="width: 100%" size="small" :header-cell-style="{ background: '#252526', color: '#b0b0b0' }" :cell-style="{ background: '#1d1e1f', color: '#e0e0e0' }">
      <el-table-column prop="symbol" label="Symbol" width="100" />
      <el-table-column prop="action" label="Action" width="80">
        <template #default="{ row }">
          <el-tag :type="row.action === 'BUY' ? 'success' : 'danger'" size="small">{{ row.action }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="strength" label="Strength" width="90">
        <template #default="{ row }">
          <el-progress :percentage="Math.round(row.strength * 100)" :stroke-width="6" :show-text="true" style="width: 80px" />
        </template>
      </el-table-column>
      <el-table-column prop="strategy_name" label="Strategy" width="120" />
      <el-table-column prop="reason" label="Reason" />
      <el-table-column prop="was_executed" label="Executed" width="90">
        <template #default="{ row }">
          <el-tag :type="row.was_executed ? 'success' : 'info'" size="small">
            {{ row.was_executed ? 'Yes' : 'No' }}
          </el-tag>
        </template>
      </el-table-column>
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
import { fetchSignals } from '@/api/signals'
import type { SignalRecord } from '@/types/models'
import { SYMBOLS } from '@/utils/constants'
import { formatTime } from '@/utils/format'

const signals = ref<SignalRecord[]>([])
const filter = ref({ symbol: '', action: '' })
const page = ref(1)
const pageSize = 20
const total = ref(0)

async function loadData() {
  try {
    const params: Record<string, any> = { limit: pageSize, offset: (page.value - 1) * pageSize }
    if (filter.value.symbol) params.symbol = filter.value.symbol
    if (filter.value.action) params.action = filter.value.action
    const res = await fetchSignals(params)
    signals.value = res.data as SignalRecord[]
    total.value = res.total
  } catch {}
}

function onPageChange(p: number) {
  page.value = p
  loadData()
}

onMounted(loadData)
</script>
