<template>
  <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
    <template #header>
      <span style="color: #e0e0e0">Account Overview</span>
    </template>
    <el-row :gutter="16">
      <el-col :span="6">
        <div class="stat-item">
          <div class="stat-label">Total Equity</div>
          <div class="stat-value">{{ formatNumber(data.total_equity) }}</div>
        </div>
      </el-col>
      <el-col :span="6">
        <div class="stat-item">
          <div class="stat-label">Free Cash</div>
          <div class="stat-value">{{ formatNumber(data.free_cash) }}</div>
        </div>
      </el-col>
      <el-col :span="6">
        <div class="stat-item">
          <div class="stat-label">Daily PnL</div>
          <div class="stat-value" :style="{ color: data.daily_pnl >= 0 ? '#67C23A' : '#F56C6C' }">
            {{ formatPnl(data.daily_pnl) }}
          </div>
        </div>
      </el-col>
      <el-col :span="6">
        <div class="stat-item">
          <div class="stat-label">Drawdown</div>
          <div class="stat-value" style="color: #E6A23C">{{ formatPercent(-data.drawdown_pct) }}</div>
        </div>
      </el-col>
    </el-row>
  </el-card>
</template>

<script setup lang="ts">
import type { Overview } from '@/types/models'
import { formatNumber, formatPnl, formatPercent } from '@/utils/format'

defineProps<{ data: Overview }>()
</script>

<style scoped>
.stat-item { text-align: center; }
.stat-label { font-size: 12px; color: #888; margin-bottom: 4px; }
.stat-value { font-size: 20px; font-weight: 600; color: #e0e0e0; }
</style>
