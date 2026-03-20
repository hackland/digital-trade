<template>
  <div>
    <!-- Config Panel -->
    <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
      <template #header>
        <span style="color: #e0e0e0">Backtest Configuration</span>
      </template>
      <el-form :inline="true" size="small" label-position="top">
        <el-form-item label="Symbol">
          <el-select v-model="form.symbol" style="width: 130px">
            <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
          </el-select>
        </el-form-item>
        <el-form-item label="Interval">
          <el-select v-model="form.interval" style="width: 100px">
            <el-option v-for="i in INTERVALS" :key="i" :label="i" :value="i" />
          </el-select>
        </el-form-item>
        <el-form-item label="Strategy">
          <el-select v-model="form.strategy" style="width: 200px">
            <el-option v-for="s in strategies" :key="s.name" :label="s.label" :value="s.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="Initial Cash">
          <el-input-number v-model="form.cash" :min="100" :step="1000" style="width: 140px" />
        </el-form-item>
        <el-form-item label="Alloc %">
          <el-input-number v-model="allocPct" :min="1" :max="100" :step="5" style="width: 100px" />
        </el-form-item>
        <el-form-item label="Fee %">
          <el-input-number v-model="feePct" :min="0" :max="100" :step="0.01" :precision="2" style="width: 110px" />
        </el-form-item>
      </el-form>

      <!-- Custom Weighted Config Panel -->
      <div v-if="isCustomWeighted" class="custom-weighted-panel">
        <div class="cw-section" v-for="cat in categoryOrder" :key="cat">
          <div class="cw-category-title">{{ categoryLabels[cat] || cat }}</div>
          <div v-for="mod in groupedModules[cat]" :key="mod.name" class="cw-module-row">
            <el-checkbox
              v-model="enabledModules[mod.name]"
              @change="onModuleToggle(mod.name)"
              class="cw-checkbox"
            >
              <span class="cw-mod-label">{{ mod.label }}</span>
            </el-checkbox>
            <el-tooltip :content="mod.description" placement="top">
              <el-icon class="cw-info-icon"><QuestionFilled /></el-icon>
            </el-tooltip>
            <div class="cw-weight-area" v-if="enabledModules[mod.name]">
              <el-slider
                v-model="moduleWeights[mod.name]"
                :min="0" :max="100" :step="5"
                :show-tooltip="false"
                style="width: 120px"
                size="small"
              />
              <span class="cw-weight-val">{{ moduleWeights[mod.name] }}%</span>
            </div>
            <!-- Module params -->
            <div class="cw-params" v-if="enabledModules[mod.name] && mod.params?.length">
              <span v-for="p in mod.params" :key="p.key" class="cw-param-item">
                <span class="cw-param-label">{{ p.label }}:</span>
                <el-input-number
                  v-model="moduleParams[mod.name][p.key]"
                  :min="p.min" :max="p.max" :step="p.step"
                  size="small"
                  controls-position="right"
                  style="width: 90px"
                />
              </span>
            </div>
          </div>
        </div>

        <!-- Signal controls - grouped -->
        <div class="cw-section">
          <div class="cw-category-title" style="display: flex; align-items: center; gap: 12px">
            信号控制
            <div class="cw-preset-btns">
              <el-button
                v-for="(preset, key) in signalPresets"
                :key="key"
                :type="activePreset === key ? 'warning' : 'default'"
                size="small"
                @click="applyPreset(key as string)"
                :style="activePreset === key
                  ? 'background: #f0b90b; border-color: #f0b90b; color: #000; font-size: 12px'
                  : 'background: #252526; border-color: #444; color: #b0b0b0; font-size: 12px'"
              >
                {{ preset.label }}
              </el-button>
            </div>
          </div>

          <div class="cw-signal-groups">
            <div v-for="grp in signalGroups" :key="grp.key" class="cw-signal-group">
              <div class="cw-group-header">{{ grp.label }}</div>
              <div class="cw-group-params">
                <div v-for="sp in getParamsByGroup(grp.key)" :key="sp.key" class="cw-signal-param">
                  <div class="cw-signal-param-top">
                    <span class="cw-signal-param-label">{{ sp.label }}</span>
                    <el-tooltip v-if="sp.desc" :content="sp.desc" placement="top" :show-after="200">
                      <el-icon class="cw-info-icon"><QuestionFilled /></el-icon>
                    </el-tooltip>
                  </div>
                  <el-switch
                    v-if="sp.type === 'bool'"
                    v-model="signalConfig[sp.key]"
                    size="small"
                    @change="activePreset = ''"
                  />
                  <el-select
                    v-else-if="sp.type === 'string'"
                    v-model="signalConfig[sp.key]"
                    size="small"
                    style="width: 80px"
                    @change="activePreset = ''"
                  >
                    <el-option label="1h" value="1h" />
                    <el-option label="4h" value="4h" />
                    <el-option label="1d" value="1d" />
                  </el-select>
                  <el-input-number
                    v-else
                    v-model="signalConfig[sp.key]"
                    :min="sp.min" :max="sp.max" :step="sp.step"
                    :precision="sp.type === 'float' ? 2 : 0"
                    size="small"
                    controls-position="right"
                    style="width: 100px"
                    @change="activePreset = ''"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- Weight summary -->
        <div class="cw-weight-summary">
          <span>权重合计: </span>
          <span :style="{ color: totalWeight === 100 ? '#67C23A' : '#F56C6C', fontWeight: 600 }">
            {{ totalWeight }}%
          </span>
          <span v-if="totalWeight !== 100" style="color: #F56C6C; margin-left: 8px; font-size: 12px">
            (建议调整至 100%)
          </span>
          <el-button size="small" text type="info" @click="resetModules" style="margin-left: 12px">重置默认</el-button>
        </div>
      </div>

      <!-- Time Range Row -->
      <div class="time-range-row">
        <div class="quick-btns">
          <el-button
            v-for="preset in dayPresets"
            :key="preset.days"
            :type="activeDays === preset.days && !useCustomRange ? 'primary' : 'default'"
            size="small"
            @click="selectPreset(preset.days)"
            :style="activeDays === preset.days && !useCustomRange
              ? 'background: #f0b90b; border-color: #f0b90b; color: #000'
              : 'background: #252526; border-color: #444; color: #b0b0b0'"
          >
            {{ preset.label }}
          </el-button>
          <el-button
            :type="useCustomRange ? 'primary' : 'default'"
            size="small"
            @click="useCustomRange = true"
            :style="useCustomRange
              ? 'background: #f0b90b; border-color: #f0b90b; color: #000'
              : 'background: #252526; border-color: #444; color: #b0b0b0'"
          >
            Custom
          </el-button>
        </div>

        <el-date-picker
          v-if="useCustomRange"
          v-model="dateRange"
          type="daterange"
          range-separator="~"
          start-placeholder="Start"
          end-placeholder="End"
          format="YYYY-MM-DD"
          value-format="YYYY-MM-DD"
          size="small"
          style="width: 280px; margin-left: 12px"
          :disabled-date="disableFutureDate"
        />

        <el-button
          type="primary"
          :loading="loading"
          @click="doBacktest"
          size="small"
          style="background: #f0b90b; border-color: #f0b90b; color: #000; margin-left: 16px; height: 32px; font-weight: 600"
        >
          Run Backtest
        </el-button>
        <el-popconfirm
          v-if="result"
          title="确认将当前策略参数部署到实盘引擎？"
          confirm-button-text="确认部署"
          cancel-button-text="取消"
          @confirm="doDeploy"
        >
          <template #reference>
            <el-button
              type="success"
              :loading="deploying"
              size="small"
              style="margin-left: 12px; height: 32px; font-weight: 600"
            >
              部署到实盘
            </el-button>
          </template>
        </el-popconfirm>
        <el-button
          size="small"
          @click="showLiveStrategy"
          :loading="loadingLive"
          style="margin-left: 12px; height: 32px"
        >
          查看实盘策略
        </el-button>
        <el-button
          size="small"
          type="warning"
          @click="showDiagnostics"
          :loading="loadingDiag"
          style="margin-left: 12px; height: 32px; font-weight: 600"
        >
          策略诊断
        </el-button>
      </div>
    </el-card>

    <!-- Diagnostics Dialog -->
    <el-dialog v-model="diagVisible" title="策略实时诊断" width="680px">
      <div v-if="diagData" style="font-size: 14px; line-height: 1.8">
        <!-- Message only (no eval yet) -->
        <template v-if="diagData.message && !diagData.timestamp">
          <div style="text-align: center; padding: 20px; color: #909399">
            {{ diagData.message }}
          </div>
        </template>
        <template v-else>
          <!-- Status Bar -->
          <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 16px; padding: 12px; background: #1d1e1f; border-radius: 8px">
            <el-tag :type="diagData.action === 'HOLD' ? 'info' : diagData.action === 'BUY' ? 'success' : 'danger'" size="large">
              {{ diagData.action }}
            </el-tag>
            <span style="color: #909399">{{ diagData.symbol }}</span>
            <span style="color: #909399; margin-left: auto; font-size: 12px">{{ formatDiagTime(diagData.timestamp) }}</span>
          </div>

          <!-- Hold Reason (most important) -->
          <el-alert
            v-if="diagData.hold_reason"
            :title="diagData.hold_reason"
            type="info"
            show-icon
            :closable="false"
            style="margin-bottom: 16px"
          />
          <el-alert
            v-if="diagData.reason"
            :title="diagData.reason"
            type="success"
            show-icon
            :closable="false"
            style="margin-bottom: 16px"
          />

          <!-- Composite Score -->
          <div style="margin-bottom: 16px">
            <div style="font-weight: 600; color: #f0b90b; margin-bottom: 8px">综合评分</div>
            <div style="display: flex; align-items: center; gap: 12px; padding: 0 12px">
              <el-progress
                :percentage="Math.min(Math.abs(diagData.composite_score) * 100, 100)"
                :color="diagData.composite_score >= 0 ? '#67C23A' : '#F56C6C'"
                :stroke-width="20"
                :text-inside="true"
                :format="() => diagData!.composite_score.toFixed(3)"
                style="flex: 1"
              />
              <span style="color: #909399; font-size: 12px; white-space: nowrap">
                买入阈值: {{ diagData.buy_threshold }} · 卖出阈值: {{ diagData.sell_threshold }}
              </span>
            </div>
          </div>

          <!-- Module Scores -->
          <div style="margin-bottom: 16px">
            <div style="font-weight: 600; color: #f0b90b; margin-bottom: 8px">各模块评分</div>
            <div v-for="(score, name) in diagData.module_scores" :key="name"
              style="display: flex; align-items: center; gap: 8px; padding: 4px 12px"
            >
              <span style="width: 120px; color: #ccc">{{ name }}</span>
              <el-progress
                :percentage="Math.min(Math.abs(score) * 100, 100)"
                :color="score >= 0 ? '#67C23A' : '#F56C6C'"
                :stroke-width="14"
                :text-inside="true"
                :format="() => score.toFixed(3)"
                style="flex: 1"
              />
              <span style="width: 40px; text-align: right; color: #909399; font-size: 12px">
                {{ ((diagData.module_weights[name] || 0) * 100).toFixed(0) }}%
              </span>
            </div>
          </div>

          <!-- Filters & State -->
          <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 12px">
            <!-- Left: Filters -->
            <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
              <div style="font-weight: 600; color: #f0b90b; margin-bottom: 8px">过滤器状态</div>
              <div class="diag-row">
                <span>趋势过滤(EMA)</span>
                <el-tag v-if="diagData.trend_filter_on" :type="diagData.trend_bullish ? 'success' : 'danger'" size="small">
                  {{ diagData.trend_bullish ? '看多' : '看空' }} ({{ diagData.trend_ema_dist_pct.toFixed(2) }}%)
                </el-tag>
                <el-tag v-else type="info" size="small">关闭</el-tag>
              </div>
              <div class="diag-row">
                <span>大周期过滤(HTF)</span>
                <el-tag v-if="diagData.htf_enabled" :type="diagData.htf_bullish ? 'success' : 'danger'" size="small">
                  {{ diagData.htf_bullish ? '看多' : '看空' }} ({{ diagData.htf_ema_dist_pct.toFixed(2) }}%)
                </el-tag>
                <el-tag v-else type="info" size="small">关闭</el-tag>
              </div>
              <div class="diag-row" v-if="diagData.htf_blocked">
                <span>HTF拦截</span>
                <el-tag type="danger" size="small">已拦截买入</el-tag>
              </div>
            </el-card>
            <!-- Right: Position State -->
            <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
              <div style="font-weight: 600; color: #f0b90b; margin-bottom: 8px">运行状态</div>
              <div class="diag-row">
                <span>持仓</span>
                <el-tag :type="diagData.has_position ? 'warning' : 'info'" size="small">
                  {{ diagData.has_position ? '是' : '否' }}
                </el-tag>
              </div>
              <div class="diag-row" v-if="!diagData.has_position">
                <span>确认进度</span>
                <span style="color: #ccc">{{ diagData.confirm_count }} / {{ diagData.confirm_bars }}</span>
              </div>
              <div class="diag-row" v-if="!diagData.has_position && diagData.cooldown_count > 0">
                <span>冷却剩余</span>
                <span style="color: #F56C6C">{{ diagData.cooldown_count }} 根K线</span>
              </div>
              <div class="diag-row" v-if="diagData.has_position">
                <span>入场价</span>
                <span style="color: #ccc">{{ diagData.entry_price.toFixed(2) }}</span>
              </div>
              <div class="diag-row" v-if="diagData.has_position">
                <span>最高价</span>
                <span style="color: #ccc">{{ diagData.high_water_mark.toFixed(2) }}</span>
              </div>
              <div class="diag-row" v-if="diagData.has_position">
                <span>止损价</span>
                <span style="color: #F56C6C">{{ diagData.stop_price.toFixed(2) }}</span>
              </div>
              <div class="diag-row" v-if="diagData.has_position">
                <span>持仓K线</span>
                <span style="color: #ccc">{{ diagData.bars_since_entry }} / min {{ diagData.min_hold_bars }}</span>
              </div>
              <div class="diag-row">
                <span>当前价</span>
                <span style="color: #f0b90b">{{ diagData.close_price.toFixed(2) }}</span>
              </div>
              <div class="diag-row" v-if="diagData.atr_value > 0">
                <span>ATR</span>
                <span style="color: #ccc">{{ diagData.atr_value.toFixed(2) }} × {{ diagData.atr_stop_mult }}</span>
              </div>
            </el-card>
          </div>
        </template>
      </div>
    </el-dialog>

    <!-- Live Strategy Dialog -->
    <el-dialog v-model="liveDialogVisible" title="当前实盘策略" width="500px">
      <div v-if="liveConfig" style="font-size: 14px; line-height: 2">
        <div style="margin-bottom: 12px; font-weight: 600; color: #f0b90b">模块配置</div>
        <div v-for="m in liveConfig.modules" :key="m.name" style="padding-left: 12px">
          {{ m.name }} — {{ (m.weight * 100).toFixed(0) }}%
        </div>
        <el-divider />
        <div style="margin-bottom: 8px; font-weight: 600; color: #f0b90b">信号参数</div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 4px 16px; padding-left: 12px">
          <span>买入阈值</span><span>{{ liveConfig.buy_threshold }}</span>
          <span>卖出阈值</span><span>{{ liveConfig.sell_threshold }}</span>
          <span>确认K线</span><span>{{ liveConfig.confirm_bars }}</span>
          <span>冷却期</span><span>{{ liveConfig.cooldown_bars }}</span>
          <span>最短持仓</span><span>{{ liveConfig.min_hold_bars }}</span>
          <span>ATR止损倍数</span><span>{{ liveConfig.atr_stop_mult }}</span>
          <span>趋势过滤</span><span>{{ liveConfig.trend_filter ? '开启' : '关闭' }}</span>
          <span>大周期过滤</span><span>{{ liveConfig.htf_enabled ? liveConfig.htf_interval + ' / EMA' + liveConfig.htf_period : '关闭' }}</span>
        </div>
      </div>
    </el-dialog>

    <!-- Results -->
    <template v-if="result">
      <!-- Metrics Summary -->
      <el-row :gutter="16" style="margin-bottom: 16px">
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Total Return</div>
              <div class="metric-value" :style="{ color: result.metrics.total_return >= 0 ? '#67C23A' : '#F56C6C' }">
                {{ result.metrics.total_return >= 0 ? '+' : '' }}{{ result.metrics.total_return.toFixed(2) }} USDT
              </div>
              <div class="metric-sub">
                {{ result.metrics.total_return_pct >= 0 ? '+' : '' }}{{ result.metrics.total_return_pct.toFixed(2) }}%
                · {{ result.metrics.total_trades }} trades
              </div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Win Rate</div>
              <div class="metric-value" style="color: #f0b90b">{{ (result.metrics.win_rate * 100).toFixed(1) }}%</div>
              <div class="metric-sub">{{ result.metrics.win_trades }}W / {{ result.metrics.lose_trades }}L ({{ result.metrics.total_trades }} total)</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Max Drawdown</div>
              <div class="metric-value" style="color: #F56C6C">-{{ result.metrics.max_drawdown_pct.toFixed(2) }}%</div>
              <div class="metric-sub">${{ result.metrics.max_drawdown.toFixed(2) }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Sharpe / Sortino</div>
              <div class="metric-value" style="color: #e0e0e0">{{ result.metrics.sharpe_ratio.toFixed(2) }} / {{ result.metrics.sortino_ratio.toFixed(2) }}</div>
              <div class="metric-sub">Profit Factor: {{ result.metrics.profit_factor.toFixed(2) }}</div>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
        <el-tabs v-model="activeChartTab" class="bt-chart-tabs">
          <el-tab-pane label="Backtest Kline" name="kline">
            <div ref="klineChartContainer" style="width: 100%; height: 420px"></div>
          </el-tab-pane>
          <el-tab-pane label="Equity Curve" name="equity">
            <div ref="equityChartContainer" style="width: 100%; height: 350px"></div>
          </el-tab-pane>
        </el-tabs>
      </el-card>

      <!-- Detail Metrics + Trades -->
      <el-row :gutter="16">
        <!-- Detail Stats -->
        <el-col :span="8">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <template #header>
              <span style="color: #e0e0e0">Performance Details</span>
            </template>
            <div class="detail-grid">
              <div class="detail-row"><span>Initial Cash</span><span>${{ result.initial_cash.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Final Equity</span><span>${{ result.metrics.final_equity.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Gross PnL</span><span :style="{ color: grossPnl >= 0 ? '#67C23A' : '#F56C6C' }">${{ grossPnl.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Fee Rate</span><span>{{ ((result.fee_rate || 0) * 100).toFixed(2) }}%</span></div>
              <div class="detail-row"><span>Alloc</span><span>{{ ((result.alloc_pct || 0) * 100).toFixed(0) }}%</span></div>
              <div class="detail-row"><span>Total Fees</span><span>${{ result.metrics.total_fees.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Avg Win</span><span style="color: #67C23A">${{ result.metrics.avg_win.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Avg Loss</span><span style="color: #F56C6C">${{ result.metrics.avg_loss.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Largest Win</span><span style="color: #67C23A">${{ result.metrics.largest_win.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Largest Loss</span><span style="color: #F56C6C">${{ result.metrics.largest_loss.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Period</span><span>{{ formatDate(result.start_time) }} ~ {{ formatDate(result.end_time) }}</span></div>
            </div>
          </el-card>
        </el-col>

        <!-- Trade List -->
        <el-col :span="16">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <template #header>
              <span style="color: #e0e0e0">Trade History ({{ result.trades?.length || 0 }})</span>
            </template>
            <el-table
              :data="result.trades"
              style="width: 100%"
              size="small"
              max-height="400"
              :header-cell-style="{ background: '#252526', color: '#b0b0b0' }"
              :cell-style="tradeCellStyle"
              @row-click="onTradeRowClick"
            >
              <el-table-column label="Time" width="160">
                <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
              </el-table-column>
              <el-table-column prop="side" label="Side" width="70">
                <template #default="{ row }">
                  <el-tag :type="row.side === 'BUY' ? 'success' : 'danger'" size="small">{{ row.side }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="Price" width="100">
                <template #default="{ row }">{{ formatPrice(row.price) }}</template>
              </el-table-column>
              <el-table-column label="Qty" width="100">
                <template #default="{ row }">{{ row.quantity.toFixed(6) }}</template>
              </el-table-column>
              <el-table-column label="Amount (U)" width="110">
                <template #default="{ row }">
                  <span :style="{ color: row.side === 'BUY' ? '#67C23A' : '#F56C6C' }">
                    {{ row.side === 'BUY' ? '-' : '+' }}{{ (row.price * row.quantity).toFixed(2) }}
                  </span>
                </template>
              </el-table-column>
              <el-table-column label="Fee (U)" width="80">
                <template #default="{ row }">{{ row.fee.toFixed(2) }}</template>
              </el-table-column>
              <el-table-column label="PnL (U)" width="100">
                <template #default="{ row }">
                  <span v-if="row.side === 'SELL'" :style="{ color: row.pnl >= 0 ? '#67C23A' : '#F56C6C' }">
                    {{ row.pnl >= 0 ? '+' : '' }}{{ row.pnl.toFixed(2) }}
                  </span>
                  <span v-else style="color: #888">-</span>
                </template>
              </el-table-column>
              <el-table-column label="Reason" min-width="200" show-overflow-tooltip>
                <template #default="{ row }">{{ row.reason }}</template>
              </el-table-column>
            </el-table>
          </el-card>
        </el-col>
      </el-row>

      <!-- Short Strategy Results -->
      <template v-if="result.short_trades?.length > 0">
        <el-divider style="border-color: #444; margin: 24px 0 16px">
          <span style="color: #f0b90b; font-size: 14px; font-weight: 600">做空策略回测结果 (仅告警，不自动交易)</span>
        </el-divider>
        <el-row :gutter="16" style="margin-bottom: 16px">
          <el-col :span="6">
            <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
              <div class="metric-card">
                <div class="metric-label">做空收益</div>
                <div class="metric-value" :style="{ color: result.short_metrics.total_return >= 0 ? '#67C23A' : '#F56C6C' }">
                  {{ result.short_metrics.total_return >= 0 ? '+' : '' }}{{ result.short_metrics.total_return.toFixed(2) }} USDT
                </div>
                <div class="metric-sub">
                  {{ result.short_metrics.total_return_pct >= 0 ? '+' : '' }}{{ result.short_metrics.total_return_pct.toFixed(2) }}%
                  · {{ result.short_metrics.total_trades }} trades
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
              <div class="metric-card">
                <div class="metric-label">做空胜率</div>
                <div class="metric-value" style="color: #f0b90b">{{ (result.short_metrics.win_rate * 100).toFixed(1) }}%</div>
                <div class="metric-sub">{{ result.short_metrics.win_trades }}W / {{ result.short_metrics.lose_trades }}L</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
              <div class="metric-card">
                <div class="metric-label">做空平均盈亏</div>
                <div class="metric-value" style="color: #67C23A">${{ result.short_metrics.avg_win.toFixed(2) }}</div>
                <div class="metric-sub">Avg Loss: ${{ result.short_metrics.avg_loss.toFixed(2) }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
              <div class="metric-card">
                <div class="metric-label">做空Profit Factor</div>
                <div class="metric-value" style="color: #e0e0e0">{{ result.short_metrics.profit_factor.toFixed(2) }}</div>
                <div class="metric-sub">Fees: ${{ result.short_metrics.total_fees.toFixed(2) }}</div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
          <template #header>
            <span style="color: #e0e0e0">做空交易记录 ({{ result.short_trades.length }})</span>
          </template>
          <el-table
            :data="result.short_trades"
            style="width: 100%"
            size="small"
            max-height="400"
            :header-cell-style="{ background: '#252526', color: '#b0b0b0' }"
            :cell-style="tradeCellStyle"
          >
            <el-table-column label="Time" width="160">
              <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
            </el-table-column>
            <el-table-column prop="side" label="Side" width="80">
              <template #default="{ row }">
                <el-tag :type="row.side === 'SHORT' ? 'warning' : 'info'" size="small">{{ row.side }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="Price" width="100">
              <template #default="{ row }">{{ formatPrice(row.price) }}</template>
            </el-table-column>
            <el-table-column label="Qty" width="100">
              <template #default="{ row }">{{ row.quantity.toFixed(6) }}</template>
            </el-table-column>
            <el-table-column label="Fee (U)" width="80">
              <template #default="{ row }">{{ row.fee.toFixed(2) }}</template>
            </el-table-column>
            <el-table-column label="PnL (U)" width="100">
              <template #default="{ row }">
                <span v-if="row.side === 'COVER'" :style="{ color: row.pnl >= 0 ? '#67C23A' : '#F56C6C' }">
                  {{ row.pnl >= 0 ? '+' : '' }}{{ row.pnl.toFixed(2) }}
                </span>
                <span v-else style="color: #888">-</span>
              </template>
            </el-table-column>
            <el-table-column label="Reason" min-width="200" show-overflow-tooltip>
              <template #default="{ row }">{{ row.reason }}</template>
            </el-table-column>
          </el-table>
        </el-card>
      </template>
    </template>

    <!-- Empty State -->
    <el-card v-else-if="!loading" shadow="never" style="background: #1d1e1f; border-color: #333; text-align: center; padding: 60px 0">
      <el-empty description="Configure parameters and click 'Run Backtest' to start" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { createChart, type IChartApi, type ISeriesApi, ColorType } from 'lightweight-charts'
import { runBacktest as apiRunBacktest, getStrategies, getIndicatorModules, deployStrategy, getStrategyDiagnostics, type BacktestRequest, type BacktestResult, type StrategyInfo, type ModuleMeta, type ParamSchema, type SignalPreset, type StrategyDiagnostics } from '@/api/backtest'
import http from '@/api/http'
import { fetchKlines } from '@/api/klines'
import { SYMBOLS, INTERVALS } from '@/utils/constants'
import { formatPrice, formatTime } from '@/utils/format'
import { ElMessage } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'

// --- Day presets ---
const dayPresets = [
  { label: '7D', days: 7 },
  { label: '30D', days: 30 },
  { label: '90D', days: 90 },
  { label: '180D', days: 180 },
  { label: '365D', days: 365 },
  { label: '2Y', days: 730 },
]

const form = ref({
  symbol: 'BTCUSDT',
  interval: '1h',
  strategy: 'custom_weighted',
  price_strategy: '',
  volume_strategy: '',
  cash: 10000,
})

const activeDays = ref(30)
const useCustomRange = ref(false)
const dateRange = ref<[string, string] | null>(null)

const allocPct = ref(100)
const feePct = ref(0.1)
const loading = ref(false)
const deploying = ref(false)
const loadingLive = ref(false)
const loadingDiag = ref(false)
const diagVisible = ref(false)
const diagData = ref<StrategyDiagnostics | null>(null)
const liveDialogVisible = ref(false)
const liveConfig = ref<Record<string, any> | null>(null)
const result = ref<BacktestResult | null>(null)
const selectedTradeIndex = ref<number | null>(null)
const strategies = ref<StrategyInfo[]>([
  { name: 'custom_weighted', label: '自定义加权 (Custom Weighted)' },
])
const isManualComposite = ref(false)
const isCustomWeighted = ref(true)

// --- Custom Weighted Module Config ---
const categoryOrder = ['trend', 'momentum', 'money_flow', 'volume']
const categoryLabels: Record<string, string> = {
  trend: '趋势类',
  momentum: '动量类',
  money_flow: '资金流类',
  volume: '成交量类',
}

const groupedModules = ref<Record<string, ModuleMeta[]>>({})
const signalParams = ref<ParamSchema[]>([])
const enabledModules = ref<Record<string, boolean>>({})
const moduleWeights = ref<Record<string, number>>({})
const moduleParams = ref<Record<string, Record<string, any>>>({})
const signalConfig = ref<Record<string, any>>({})
const signalPresets = ref<Record<string, SignalPreset>>({})
const activePreset = ref('')

const signalGroups = [
  { key: 'signal', label: '买卖信号' },
  { key: 'position', label: '持仓控制' },
  { key: 'stoploss', label: '止损' },
  { key: 'trend', label: '趋势过滤' },
  { key: 'short', label: '做空策略 (仅告警)' },
]

function getParamsByGroup(group: string): ParamSchema[] {
  return signalParams.value.filter(sp => sp.group === group)
}

function applyPreset(key: string) {
  const preset = signalPresets.value[key]
  if (!preset) return
  for (const sp of signalParams.value) {
    if (sp.key in preset) {
      signalConfig.value[sp.key] = preset[sp.key]
    }
  }
  activePreset.value = key
}

const grossPnl = computed(() => {
  if (!result.value?.trades) return 0
  return result.value.trades.reduce((sum, t) => sum + (t.pnl || 0), 0)
})

const totalWeight = computed(() => {
  let sum = 0
  for (const [name, enabled] of Object.entries(enabledModules.value)) {
    if (enabled) sum += (moduleWeights.value[name] || 0)
  }
  return sum
})

let allModules: ModuleMeta[] = []

const fallbackSignalParams: ParamSchema[] = [
  { key: 'buy_threshold', label: '买入阈值', type: 'float', default: 0.20, min: 0.05, max: 0.8, step: 0.05, group: 'signal', desc: '综合评分超过此值才触发买入，越高越严格' },
  { key: 'sell_threshold', label: '卖出阈值', type: 'float', default: -0.30, min: -1.0, max: -0.1, step: 0.05, group: 'signal', desc: '综合评分低于此值触发卖出，越低越宽松' },
  { key: 'confirm_bars', label: '确认K线数', type: 'int', default: 1, min: 1, max: 5, step: 1, group: 'signal', desc: '连续N根K线评分达标才买入，防止假信号' },
  { key: 'cooldown_bars', label: '冷却期', type: 'int', default: 12, min: 0, max: 48, step: 1, group: 'position', desc: '卖出后等待N根K线才允许再次买入' },
  { key: 'min_hold_bars', label: '最短持仓', type: 'int', default: 6, min: 0, max: 30, step: 1, group: 'position', desc: '买入后至少持有N根K线，避免频繁交易' },
  { key: 'atr_stop_mult', label: 'ATR止损倍数', type: 'float', default: 3.0, min: 1.0, max: 6.0, step: 0.1, group: 'stoploss', desc: '追踪止损距离 = ATR × 倍数，越大越宽松' },
  { key: 'trend_filter', label: 'EMA趋势过滤', type: 'bool', default: false, min: 0, max: 1, step: 1, group: 'trend', desc: '开启后只在价格高于EMA均线时买入，过滤下跌趋势' },
  { key: 'trend_period', label: 'EMA周期', type: 'int', default: 50, min: 20, max: 200, step: 5, group: 'trend', desc: '趋势判断用的均线周期，50表示看50根K线趋势' },
  { key: 'htf_enabled', label: '大周期过滤', type: 'bool', default: true, min: 0, max: 1, step: 1, group: 'trend', desc: '开启后用更大时间周期(如4h)确认趋势方向' },
  { key: 'htf_interval', label: '大周期', type: 'string', default: '1d', min: 0, max: 0, step: 0, group: 'trend', desc: '用于趋势确认的大时间框架' },
  { key: 'htf_period', label: '大周期EMA', type: 'int', default: 10, min: 10, max: 100, step: 5, group: 'trend', desc: '大周期上的EMA均线周期' },
]

const fallbackPresets: Record<string, SignalPreset> = {
  conservative: { label: '保守', desc: '低频交易，高确认，严格过滤', buy_threshold: 0.30, sell_threshold: -0.30, confirm_bars: 2, cooldown_bars: 24, min_hold_bars: 12, atr_stop_mult: 3.5, trend_filter: false, trend_period: 50, htf_enabled: true, htf_interval: '1d', htf_period: 10 },
  standard: { label: '标准', desc: '推荐配置: EMA+MACD+MFI, 日线过滤', buy_threshold: 0.20, sell_threshold: -0.30, confirm_bars: 1, cooldown_bars: 12, min_hold_bars: 6, atr_stop_mult: 3.0, trend_filter: false, trend_period: 50, htf_enabled: true, htf_interval: '1d', htf_period: 10 },
  aggressive: { label: '激进', desc: '低阈值、快进快出，交易频率高', buy_threshold: 0.10, sell_threshold: -0.15, confirm_bars: 1, cooldown_bars: 2, min_hold_bars: 3, atr_stop_mult: 2.0, trend_filter: false, trend_period: 30, htf_enabled: true, htf_interval: '1d', htf_period: 10 },
}

function mergeSignalParams(apiParams: ParamSchema[]): ParamSchema[] {
  // Merge group/desc/label from fallback if API doesn't have them
  const fallbackMap = new Map(fallbackSignalParams.map(p => [p.key, p]))
  return apiParams.map(sp => {
    const fb = fallbackMap.get(sp.key)
    if (!fb) return { ...sp, group: sp.group || 'signal', desc: sp.desc || '' }
    return {
      ...sp,
      label: fb.label,  // always prefer our cleaner labels
      group: sp.group || fb.group || 'signal',
      desc: sp.desc || fb.desc || '',
    }
  })
}

async function loadModules() {
  try {
    const data = await getIndicatorModules()
    allModules = data.modules
    groupedModules.value = data.grouped
    signalParams.value = data.signal_params?.length
      ? mergeSignalParams(data.signal_params)
      : fallbackSignalParams
    signalPresets.value = data.signal_presets || fallbackPresets
  } catch {
    signalParams.value = fallbackSignalParams
    signalPresets.value = fallbackPresets
  }
  resetModules()
  activePreset.value = 'standard'
}

function resetModules() {
  enabledModules.value = {}
  moduleWeights.value = {}
  moduleParams.value = {}
  signalConfig.value = {}

  // 推荐策略: EMA金叉40% + MACD40% + MFI资金流量20%
  const defaultModules: Record<string, number> = {
    'ema_cross': 40,
    'macd': 40,
    'mfi': 20,
  }
  for (const mod of allModules) {
    enabledModules.value[mod.name] = mod.name in defaultModules
    moduleWeights.value[mod.name] = defaultModules[mod.name] ?? Math.round(mod.default_weight * 100)
    const params: Record<string, any> = {}
    for (const p of (mod.params || [])) {
      params[p.key] = p.default
    }
    moduleParams.value[mod.name] = params
  }

  for (const sp of signalParams.value) {
    signalConfig.value[sp.key] = sp.default
  }
}

function onModuleToggle(name: string) {
  if (!enabledModules.value[name]) {
    // Just disabled, weight stays for re-enable
  }
}

function buildStrategyConfig(): Record<string, any> {
  const mods: any[] = []
  for (const [name, enabled] of Object.entries(enabledModules.value)) {
    if (!enabled) continue
    const weight = (moduleWeights.value[name] || 0) / 100
    if (weight <= 0) continue
    mods.push({
      name,
      weight,
      params: moduleParams.value[name] || {},
    })
  }
  return {
    modules: mods,
    ...signalConfig.value,
  }
}

const activeChartTab = ref<'kline' | 'equity'>('kline')
const equityChartContainer = ref<HTMLElement | null>(null)
const klineChartContainer = ref<HTMLElement | null>(null)
let equityChart: IChartApi | null = null
let klineChart: IChartApi | null = null
let equitySeries: ISeriesApi<'Area'> | null = null
let klineCandleSeries: ISeriesApi<'Candlestick'> | null = null
let klineVolumeSeries: ISeriesApi<'Histogram'> | null = null
let equityResizeObserver: ResizeObserver | null = null
let klineResizeObserver: ResizeObserver | null = null
let backtestTradeMarkers: any[] = []

function selectPreset(days: number) {
  activeDays.value = days
  useCustomRange.value = false
  dateRange.value = null
}

function disableFutureDate(date: Date): boolean {
  return date.getTime() > Date.now()
}

function formatDate(s: string): string {
  const d = new Date(s)
  if (d.getFullYear() <= 1) return 'N/A'
  return d.toLocaleDateString()
}

async function doBacktest() {
  loading.value = true
  result.value = null
  selectedTradeIndex.value = null
  backtestTradeMarkers = []
  try {
    const req: BacktestRequest = {
      ...form.value,
      alloc: allocPct.value / 100,
      fee: feePct.value / 100,
    }
    delete req.price_strategy
    delete req.volume_strategy
    if (form.value.strategy === 'custom_weighted') {
      req.strategy_config = buildStrategyConfig()
    }

    if (useCustomRange.value && dateRange.value) {
      req.start = dateRange.value[0]
      req.end = dateRange.value[1]
    } else {
      req.days = activeDays.value
    }

    const res = await apiRunBacktest(req)
    result.value = res
    await nextTick()
    await renderActiveChart()
    ElMessage.success(`Backtest complete: ${res.metrics.total_trades} trades, ${res.metrics.total_return_pct.toFixed(2)}% return`)
  } catch (e: any) {
    ElMessage.error('Backtest failed: ' + (e.response?.data?.message || e.message))
  } finally {
    loading.value = false
  }
}

async function doDeploy() {
  deploying.value = true
  try {
    const config = buildStrategyConfig()
    const mods = config.modules.map((m: any) => ({ name: m.name, weight: m.weight }))
    const signalParams: Record<string, any> = { ...config }
    delete signalParams.modules

    await deployStrategy({ modules: mods, signal_params: signalParams })
    ElMessage.success('策略已部署到实盘引擎')
  } catch (e: any) {
    ElMessage.error('部署失败: ' + (e.response?.data?.message || e.message))
  } finally {
    deploying.value = false
  }
}

async function showLiveStrategy() {
  loadingLive.value = true
  try {
    const res = await http.get('/strategy/status')
    liveConfig.value = res.data.data.config
    liveDialogVisible.value = true
  } catch (e: any) {
    ElMessage.error('获取失败: ' + (e.response?.data?.message || e.message))
  } finally {
    loadingLive.value = false
  }
}

async function showDiagnostics() {
  loadingDiag.value = true
  try {
    diagData.value = await getStrategyDiagnostics()
    diagVisible.value = true
  } catch (e: any) {
    ElMessage.error('获取失败: ' + (e.response?.data?.message || e.message))
  } finally {
    loadingDiag.value = false
  }
}

function formatDiagTime(ts: string) {
  if (!ts) return ''
  const d = new Date(ts)
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

const tzOffsetSec = -new Date().getTimezoneOffset() * 60

function toLocalChartTime(isoOrTs: string | number): number {
  const utcSec = Math.floor(new Date(isoOrTs).getTime() / 1000)
  return utcSec + tzOffsetSec
}

function volumeColor(open: number, close: number): string {
  return close >= open ? 'rgba(103,194,58,0.4)' : 'rgba(245,108,108,0.4)'
}

function intervalSeconds(interval: string): number {
  switch (interval) {
    case '1m': return 60
    case '3m': return 180
    case '5m': return 300
    case '15m': return 900
    case '30m': return 1800
    case '1h': return 3600
    case '2h': return 7200
    case '4h': return 14400
    case '6h': return 21600
    case '8h': return 28800
    case '12h': return 43200
    case '1d': return 86400
    default: return 300
  }
}

function cleanupEquityChart() {
  if (equityResizeObserver && equityChartContainer.value) {
    equityResizeObserver.unobserve(equityChartContainer.value)
    equityResizeObserver.disconnect()
  }
  equityResizeObserver = null
  equityChart?.remove()
  equityChart = null
  equitySeries = null
}

function cleanupKlineChart() {
  if (klineResizeObserver && klineChartContainer.value) {
    klineResizeObserver.unobserve(klineChartContainer.value)
    klineResizeObserver.disconnect()
  }
  klineResizeObserver = null
  klineChart?.remove()
  klineChart = null
  klineCandleSeries = null
  klineVolumeSeries = null
  backtestTradeMarkers = []
}

function buildTradeMarkers() {
  if (!result.value?.trades?.length) {
    backtestTradeMarkers = []
    return
  }
  backtestTradeMarkers = result.value.trades.map((t, idx) => {
    const isBuy = t.side === 'BUY'
    return {
      id: `${t.side}-${idx}`,
      time: toLocalChartTime(t.timestamp) as any,
      position: (isBuy ? 'belowBar' : 'aboveBar') as any,
      color: isBuy ? '#67C23A' : '#F56C6C',
      shape: (isBuy ? 'arrowUp' : 'arrowDown') as any,
      text: isBuy ? 'B' : 'S',
    }
  })
}

function applyTradeMarkers() {
  if (!klineCandleSeries) return
  const selected = selectedTradeIndex.value
  const markers = backtestTradeMarkers.map((m, idx) => {
    if (selected === idx) {
      return {
        ...m,
        color: m.shape === 'arrowUp' ? '#22c55e' : '#ef4444',
        text: `${m.text}*`,
      }
    }
    return m
  })
  klineCandleSeries.setMarkers(markers)
}

function onTradeRowClick(row: BacktestResult['trades'][number]) {
  if (!result.value?.trades?.length) return
  const idx = result.value.trades.indexOf(row)
  if (idx < 0) return
  selectedTradeIndex.value = idx
  activeChartTab.value = 'kline'
  nextTick(async () => {
    if (!klineCandleSeries) {
      await renderBacktestKlineChart()
    } else {
      applyTradeMarkers()
    }
  })
}

function tradeCellStyle({ rowIndex }: { rowIndex: number }) {
  if (selectedTradeIndex.value === rowIndex) {
    return { background: '#2a2412', color: '#f0b90b' }
  }
  return { background: '#1d1e1f', color: '#e0e0e0' }
}

async function renderActiveChart() {
  if (!result.value) return
  if (activeChartTab.value === 'kline') {
    await renderBacktestKlineChart()
  } else {
    renderEquityChart()
  }
}

function renderEquityChart() {
  if (!equityChartContainer.value || !result.value?.equity_curve?.length) return
  cleanupEquityChart()

  equityChart = createChart(equityChartContainer.value, {
    width: equityChartContainer.value.clientWidth,
    height: 350,
    layout: {
      background: { type: ColorType.Solid, color: '#1d1e1f' },
      textColor: '#b0b0b0',
    },
    grid: {
      vertLines: { color: '#2a2a2a' },
      horzLines: { color: '#2a2a2a' },
    },
    timeScale: { timeVisible: true, secondsVisible: false },
    rightPriceScale: { borderColor: '#333' },
  })

  equitySeries = equityChart.addAreaSeries({
    lineColor: '#f0b90b',
    topColor: 'rgba(240, 185, 11, 0.3)',
    bottomColor: 'rgba(240, 185, 11, 0.02)',
    lineWidth: 2,
    priceFormat: { type: 'price', precision: 2, minMove: 0.01 },
  })

  const data = result.value.equity_curve.map(p => ({
    time: toLocalChartTime(p.time) as any,
    value: p.equity,
  }))
  equitySeries.setData(data)
  equityChart.timeScale().fitContent()

  equityResizeObserver = new ResizeObserver(() => {
    if (equityChart && equityChartContainer.value) {
      equityChart.applyOptions({ width: equityChartContainer.value.clientWidth })
    }
  })
  equityResizeObserver.observe(equityChartContainer.value)
}

async function renderBacktestKlineChart() {
  if (!klineChartContainer.value || !result.value) return
  cleanupKlineChart()

  klineChart = createChart(klineChartContainer.value, {
    width: klineChartContainer.value.clientWidth,
    height: 420,
    layout: {
      background: { type: ColorType.Solid, color: '#1d1e1f' },
      textColor: '#b0b0b0',
    },
    grid: {
      vertLines: { color: '#2a2a2a' },
      horzLines: { color: '#2a2a2a' },
    },
    crosshair: { mode: 0 },
    timeScale: { timeVisible: true, secondsVisible: false },
    rightPriceScale: { borderColor: '#333' },
  })

  klineCandleSeries = klineChart.addCandlestickSeries({
    upColor: '#67C23A',
    downColor: '#F56C6C',
    borderUpColor: '#67C23A',
    borderDownColor: '#F56C6C',
    wickUpColor: '#67C23A',
    wickDownColor: '#F56C6C',
    priceScaleId: 'right',
  })
  klineCandleSeries.priceScale().applyOptions({
    scaleMargins: { top: 0.05, bottom: 0.3 },
  })

  klineVolumeSeries = klineChart.addHistogramSeries({
    priceFormat: { type: 'volume' },
    priceScaleId: 'volume',
  })
  klineVolumeSeries.priceScale().applyOptions({
    scaleMargins: { top: 0.75, bottom: 0 },
  })

  const startMs = new Date(result.value.start_time).getTime()
  const endMs = new Date(result.value.end_time).getTime()
  const intervalSec = intervalSeconds(result.value.interval)
  const estimatedBars = Math.ceil((endMs - startMs) / 1000 / intervalSec)
  const limit = Math.min(Math.max(estimatedBars + 10, 800), 10000)

  const klines = await fetchKlines({
    symbol: result.value.symbol,
    interval: result.value.interval,
    start: result.value.start_time,
    end: result.value.end_time,
    limit,
  })

  const candleData: any[] = []
  const volData: any[] = []
  for (const k of klines) {
    const t = toLocalChartTime(k.open_time)
    candleData.push({
      time: t as any,
      open: k.open,
      high: k.high,
      low: k.low,
      close: k.close,
    })
    volData.push({
      time: t as any,
      value: k.volume,
      color: volumeColor(k.open, k.close),
    })
  }

  klineCandleSeries.setData(candleData)
  klineVolumeSeries.setData(volData)

  buildTradeMarkers()
  applyTradeMarkers()
  klineChart.timeScale().fitContent()

  if (estimatedBars > 10000) {
    ElMessage.warning(`K线数量较多（约 ${estimatedBars} 根），图表已限制到 10000 根以保证性能。`)
  }

  klineResizeObserver = new ResizeObserver(() => {
    if (klineChart && klineChartContainer.value) {
      klineChart.applyOptions({ width: klineChartContainer.value.clientWidth })
    }
  })
  klineResizeObserver.observe(klineChartContainer.value)
}

onMounted(async () => {
  try {
    const list = await getStrategies()
    if (list?.length) strategies.value = list
  } catch {}
  await loadModules()
  isManualComposite.value = false
  isCustomWeighted.value = true
})

watch(() => form.value.strategy, (next) => {
  isManualComposite.value = false
  isCustomWeighted.value = next === 'custom_weighted'
})

watch(activeChartTab, async () => {
  await nextTick()
  await renderActiveChart()
})

onBeforeUnmount(() => {
  cleanupEquityChart()
  cleanupKlineChart()
})
</script>

<style scoped>
.time-range-row {
  display: flex;
  align-items: center;
  margin-top: 8px;
  padding-top: 12px;
  border-top: 1px solid #333;
}

.quick-btns {
  display: flex;
  gap: 6px;
}

.bt-chart-tabs :deep(.el-tabs__item) {
  color: #b0b0b0;
}

.bt-chart-tabs :deep(.el-tabs__item.is-active) {
  color: #f0b90b;
}

.metric-card {
  text-align: center;
  padding: 8px 0;
}
.metric-label {
  color: #888;
  font-size: 12px;
  margin-bottom: 4px;
}
.metric-value {
  font-size: 24px;
  font-weight: 600;
}
.metric-sub {
  color: #888;
  font-size: 12px;
  margin-top: 4px;
}

.detail-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.detail-row {
  display: flex;
  justify-content: space-between;
  color: #e0e0e0;
  font-size: 13px;
}
.detail-row span:first-child {
  color: #888;
}

.diag-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 3px 0;
  color: #ccc;
  font-size: 13px;
}
.diag-row span:first-child {
  color: #909399;
}

:deep(.el-form-item__label) {
  color: #b0b0b0 !important;
  font-size: 12px !important;
}
:deep(.el-input__inner),
:deep(.el-input-number__decrease),
:deep(.el-input-number__increase) {
  background: #252526 !important;
  color: #e0e0e0 !important;
  border-color: #444 !important;
}
:deep(.el-select .el-input__inner) {
  background: #252526 !important;
  color: #e0e0e0 !important;
}
:deep(.el-empty__description p) {
  color: #888 !important;
}
/* Date picker dark theme & readability */
:deep(.el-date-editor) {
  --el-date-editor-width: 280px;
  background: #252526 !important;
  border-radius: 4px !important;
}
:deep(.el-date-editor.el-input__wrapper) {
  background: #252526 !important;
  box-shadow: 0 0 0 1px #444 inset !important;
}
:deep(.el-range-editor.el-input__wrapper) {
  background: #252526 !important;
  box-shadow: 0 0 0 1px #444 inset !important;
  height: 32px !important;
}
:deep(.el-range-input) {
  background: transparent !important;
  color: #e0e0e0 !important;
  font-size: 13px !important;
}
:deep(.el-range-separator) {
  color: #b0b0b0 !important;
  font-size: 12px !important;
}
:deep(.el-date-editor .el-range-input::placeholder) {
  color: #777 !important;
}

/* Custom Weighted Panel */
.custom-weighted-panel {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid #333;
}

.cw-section {
  margin-bottom: 12px;
}

.cw-category-title {
  color: #f0b90b;
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 6px;
  padding-left: 2px;
}

.cw-module-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0 4px 8px;
  flex-wrap: wrap;
}

.cw-checkbox :deep(.el-checkbox__label) {
  color: #e0e0e0 !important;
  font-size: 13px;
}

.cw-mod-label {
  min-width: 100px;
  display: inline-block;
}

.cw-info-icon {
  color: #666;
  font-size: 14px;
  cursor: help;
}

.cw-weight-area {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: 4px;
}

.cw-weight-val {
  color: #f0b90b;
  font-size: 12px;
  min-width: 32px;
  text-align: right;
}

.cw-params {
  display: flex;
  gap: 10px;
  margin-left: 12px;
}

.cw-param-item {
  display: flex;
  align-items: center;
  gap: 4px;
}

.cw-param-label {
  color: #888;
  font-size: 12px;
  white-space: nowrap;
}

/* Signal groups layout */
.cw-preset-btns {
  display: flex;
  gap: 6px;
}

.cw-signal-groups {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 12px;
  padding: 4px 0;
}

.cw-signal-group {
  background: #252526;
  border: 1px solid #333;
  border-radius: 6px;
  padding: 10px 12px;
}

.cw-group-header {
  color: #b0b0b0;
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 8px;
  padding-bottom: 4px;
  border-bottom: 1px solid #333;
}

.cw-group-params {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.cw-signal-param {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.cw-signal-param-top {
  display: flex;
  align-items: center;
  gap: 4px;
}

.cw-signal-param-label {
  color: #e0e0e0;
  font-size: 13px;
  white-space: nowrap;
}

.cw-signal-row {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  padding-left: 8px;
}

.cw-weight-summary {
  display: flex;
  align-items: center;
  padding: 8px 8px 0 8px;
  margin-top: 4px;
  border-top: 1px solid #333;
  color: #b0b0b0;
  font-size: 13px;
}

:deep(.cw-module-row .el-slider__runway) {
  background-color: #333;
}
:deep(.cw-module-row .el-slider__bar) {
  background-color: #f0b90b;
}
:deep(.cw-module-row .el-slider__button) {
  border-color: #f0b90b;
  width: 12px;
  height: 12px;
}
</style>
