<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { ElNotification } from 'element-plus'
import Sidebar from './Sidebar.vue'
import Header from './Header.vue'
import { useWebSocket } from '@/composables/useWebSocket'
import PositionAnalysisDrawer from '@/components/dashboard/PositionAnalysisDrawer.vue'

const alertDrawerVisible = ref(false)
const alertSymbol = ref('')

let wsUnsub: (() => void) | null = null

// 持仓风险告警弹窗暂时关闭（提示太频繁）。
// 需要恢复时把下面这段取消注释即可，后端 WS 推送和 alert 抽屉不受影响。
onMounted(() => {
  // const ws = useWebSocket()
  // wsUnsub = ws.subscribe('position_alert', (data: any) => {
  //   const symbol: string = data?.symbol ?? ''
  //   const label: string = data?.regime_label ?? ''
  //   const recommendation: string = data?.recommendation ?? ''
  //   const summary: string = data?.reason_summary ?? ''
  //
  //   const recMap: Record<string, string> = {
  //     consider_close: '建议考虑平仓',
  //     close_now:      '建议立即平仓',
  //   }
  //   const title = `⚠️ 持仓风险告警 · ${symbol}`
  //   const msg = `${recMap[recommendation] ?? recommendation}：${summary}（${label}）`
  //
  //   ElNotification({
  //     title,
  //     message: msg,
  //     type: recommendation === 'close_now' ? 'error' : 'warning',
  //     duration: 0,
  //     position: 'bottom-right',
  //     onClick() {
  //       alertSymbol.value = symbol
  //       alertDrawerVisible.value = true
  //     },
  //   })
  // })
})

onUnmounted(() => { wsUnsub?.() })
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
