<template>
  <div style="display: flex; align-items: center; justify-content: space-between; width: 100%; color: #e0e0e0">
    <div style="display: flex; align-items: center; gap: 16px">
      <span style="font-size: 14px; color: #888">BTC/USDT</span>
      <span style="font-size: 18px; font-weight: 600; color: #f0b90b">{{ price }}</span>
    </div>
    <div style="display: flex; align-items: center; gap: 8px">
      <span :style="{ color: wsConnected ? '#67C23A' : '#F56C6C' }">&#9679;</span>
      <span style="font-size: 12px; color: #888">{{ wsConnected ? 'Connected' : 'Disconnected' }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useWebSocket } from '@/composables/useWebSocket'

const ws = useWebSocket()
const wsConnected = ws.connected
const price = ref('--')

let unsub: (() => void) | null = null

onMounted(() => {
  unsub = ws.subscribe('ticker', (data: any) => {
    const p = data?.price
    if (p) {
      price.value = Number(p).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    }
  })
})

onUnmounted(() => unsub?.())
</script>
