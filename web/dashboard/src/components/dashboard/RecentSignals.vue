<template>
  <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
    <template #header>
      <span style="color: #e0e0e0">Recent Signals</span>
    </template>
    <div v-for="sig in signals" :key="sig.id" style="padding: 8px 0; border-bottom: 1px solid #333">
      <div style="display: flex; justify-content: space-between; align-items: center">
        <div>
          <el-tag :type="sig.action === 'BUY' ? 'success' : 'danger'" size="small">{{ sig.action }}</el-tag>
          <span style="margin-left: 8px; color: #e0e0e0">{{ sig.symbol }}</span>
        </div>
        <span style="color: #888; font-size: 12px">{{ formatTime(sig.timestamp) }}</span>
      </div>
      <div style="color: #888; font-size: 12px; margin-top: 4px">{{ sig.reason }}</div>
    </div>
    <el-empty v-if="signals.length === 0" description="No recent signals" :image-size="60" />
  </el-card>
</template>

<script setup lang="ts">
import type { SignalRecord } from '@/types/models'
import { formatTime } from '@/utils/format'

defineProps<{ signals: SignalRecord[] }>()
</script>
