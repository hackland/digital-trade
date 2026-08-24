<script setup lang="ts">
import { ref } from 'vue'
import Sidebar from './Sidebar.vue'
import Header from './Header.vue'
import PositionAnalysisDrawer from '@/components/dashboard/PositionAnalysisDrawer.vue'

const alertDrawerVisible = ref(false)
const alertSymbol = ref('')

// 持仓风险告警弹窗暂时关闭（提示太频繁）。恢复时参考 git history 里
// 这段 onMounted 订阅 'position_alert' 并弹 ElNotification 的实现，
// 点击后设置 alertSymbol/alertDrawerVisible 打开下面的抽屉。
</script>

<template>
  <el-container style="height: 100vh">
    <el-aside width="200px" style="background: #1d1e1f">
      <Sidebar />
    </el-aside>
    <el-container>
      <el-header style="background: #1d1e1f; border-bottom: 1px solid #333; display: flex; align-items: center">
        <Header />
      </el-header>
      <el-main style="background: #141414; padding: 20px; overflow-y: auto">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
  <PositionAnalysisDrawer v-model="alertDrawerVisible" :symbol="alertSymbol" />
</template>

<style>
body {
  margin: 0;
  background: #141414;
  color: #e0e0e0;
}
.el-aside {
  border-right: 1px solid #333;
}
</style>
