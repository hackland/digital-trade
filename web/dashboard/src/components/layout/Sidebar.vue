<template>
  <div style="padding: 20px 0; text-align: center">
    <h3 style="color: #f0b90b; margin: 0 0 20px">BTC Trader</h3>
  </div>
  <el-menu :default-active="route.path" :router="true" background-color="#1d1e1f" text-color="#b0b0b0" active-text-color="#f0b90b">
    <el-menu-item index="/">
      <el-icon><Odometer /></el-icon>
      <span>Dashboard</span>
    </el-menu-item>
    <el-menu-item index="/market">
      <el-icon><TrendCharts /></el-icon>
      <span>Market</span>
    </el-menu-item>
    <el-menu-item index="/orders">
      <el-icon><List /></el-icon>
      <span>Orders</span>
    </el-menu-item>
    <el-menu-item index="/trades">
      <el-icon><Tickets /></el-icon>
      <span>Trades</span>
    </el-menu-item>
    <el-menu-item index="/signals">
      <el-icon><Aim /></el-icon>
      <span>Signals</span>
    </el-menu-item>
    <el-menu-item index="/backtest">
      <el-icon><DataAnalysis /></el-icon>
      <span>Backtest</span>
    </el-menu-item>
    <el-menu-item index="/risk">
      <el-icon><Warning /></el-icon>
      <span>Risk</span>
    </el-menu-item>
    <el-menu-item index="/settings">
      <el-icon><Setting /></el-icon>
      <span>Settings</span>
    </el-menu-item>
  </el-menu>
</template>

<script setup lang="ts">
import { useRoute } from 'vue-router'
import { Odometer, TrendCharts, List, Tickets, Aim, DataAnalysis, Warning, Setting } from '@element-plus/icons-vue'

const route = useRoute()
</script>
