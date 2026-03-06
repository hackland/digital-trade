<template>
  <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
    <template #header><span style="color: #e0e0e0">System Configuration (Read-Only)</span></template>
    <div v-if="config">
      <h4 style="color: #f0b90b; margin-top: 0">Application</h4>
      <el-descriptions :column="2" border size="small">
        <el-descriptions-item label="Name">{{ config.app?.name }}</el-descriptions-item>
        <el-descriptions-item label="Mode">
          <el-tag :type="config.app?.mode === 'live' ? 'danger' : 'info'" size="small">{{ config.app?.mode }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="Testnet">{{ config.app?.testnet ? 'Yes' : 'No' }}</el-descriptions-item>
        <el-descriptions-item label="Log Level">{{ config.app?.log_level }}</el-descriptions-item>
      </el-descriptions>

      <h4 style="color: #f0b90b">Exchange</h4>
      <el-descriptions :column="2" border size="small">
        <el-descriptions-item label="Name">{{ config.exchange?.name }}</el-descriptions-item>
        <el-descriptions-item label="Market Type">{{ config.exchange?.market_type }}</el-descriptions-item>
        <el-descriptions-item label="Symbols">{{ config.exchange?.symbols?.join(', ') }}</el-descriptions-item>
        <el-descriptions-item label="API Key">{{ config.exchange?.api_key }}</el-descriptions-item>
      </el-descriptions>

      <h4 style="color: #f0b90b">Strategy</h4>
      <el-descriptions :column="2" border size="small">
        <el-descriptions-item label="Name">{{ config.strategy?.name }}</el-descriptions-item>
        <el-descriptions-item v-for="(v, k) in config.strategy?.config" :key="String(k)" :label="String(k)">
          {{ v }}
        </el-descriptions-item>
      </el-descriptions>

      <h4 style="color: #f0b90b">Risk Parameters</h4>
      <el-descriptions :column="2" border size="small">
        <el-descriptions-item v-for="(v, k) in config.risk" :key="String(k)" :label="String(k)">
          {{ v }}
        </el-descriptions-item>
      </el-descriptions>
    </div>
    <el-empty v-else description="Loading configuration..." />
  </el-card>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { fetchConfig } from '@/api/config'

const config = ref<Record<string, any> | null>(null)

onMounted(async () => {
  try {
    config.value = await fetchConfig()
  } catch {}
})
</script>
