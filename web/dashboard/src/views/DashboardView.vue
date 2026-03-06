<template>
  <div>
    <AccountCard v-if="overview" :data="overview" />
    <el-row :gutter="16" style="margin-top: 16px">
      <el-col :span="14">
        <PositionCard :positions="overview?.positions ?? []" />
      </el-col>
      <el-col :span="10">
        <RecentSignals :signals="signals" />
      </el-col>
    </el-row>
    <el-row :gutter="16" style="margin-top: 16px">
      <el-col :span="24">
        <RecentTrades :trades="trades" />
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import AccountCard from '@/components/dashboard/AccountCard.vue'
import PositionCard from '@/components/dashboard/PositionCard.vue'
import RecentSignals from '@/components/dashboard/RecentSignals.vue'
import RecentTrades from '@/components/dashboard/RecentTrades.vue'
import { fetchOverview } from '@/api/overview'
import { fetchSignals } from '@/api/signals'
import { fetchTrades } from '@/api/trades'
import type { Overview, SignalRecord, TradeRecord } from '@/types/models'

const overview = ref<Overview | null>(null)
const signals = ref<SignalRecord[]>([])
const trades = ref<TradeRecord[]>([])

onMounted(async () => {
  try {
    overview.value = await fetchOverview()
  } catch { /* backend may not be ready */ }
  try {
    const sigRes = await fetchSignals({ limit: 10 })
    signals.value = sigRes.data as SignalRecord[]
  } catch {}
  try {
    const tradeRes = await fetchTrades({ limit: 10 })
    trades.value = tradeRes.data as TradeRecord[]
  } catch {}
})
</script>
