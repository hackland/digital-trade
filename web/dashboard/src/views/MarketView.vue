<template>
  <div>
    <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
      <div style="display: flex; gap: 16px; align-items: center">
        <el-select v-model="symbol" style="width: 160px" size="default">
          <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
        </el-select>
        <el-radio-group v-model="interval" size="small">
          <el-radio-button v-for="i in INTERVALS" :key="i" :value="i">{{ i }}</el-radio-button>
        </el-radio-group>
      </div>
    </el-card>
    <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
      <KlineChart :symbol="symbol" :interval="interval" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import KlineChart from '@/components/charts/KlineChart.vue'
import { SYMBOLS, INTERVALS } from '@/utils/constants'

const symbol = ref('BTCUSDT')
const interval = ref('5m')
</script>
