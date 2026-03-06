import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      component: () => import('@/components/layout/AppLayout.vue'),
      children: [
        { path: '', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
        { path: 'market', name: 'market', component: () => import('@/views/MarketView.vue') },
        { path: 'orders', name: 'orders', component: () => import('@/views/OrdersView.vue') },
        { path: 'trades', name: 'trades', component: () => import('@/views/TradesView.vue') },
        { path: 'signals', name: 'signals', component: () => import('@/views/SignalsView.vue') },
        { path: 'backtest', name: 'backtest', component: () => import('@/views/BacktestView.vue') },
        { path: 'risk', name: 'risk', component: () => import('@/views/RiskView.vue') },
        { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue') },
      ],
    },
  ],
})

export default router
