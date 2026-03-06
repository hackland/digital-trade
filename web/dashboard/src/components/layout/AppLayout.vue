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
</template>

<script setup lang="ts">
import Sidebar from './Sidebar.vue'
import Header from './Header.vue'
</script>

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
