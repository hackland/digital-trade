<template>
  <!-- 设置条件触发弹窗 -->
  <el-dialog
    v-model="visible"
    title="设置条件触发"
    width="440px"
    :close-on-click-modal="false"
    draggable
    style="background: #1d1e1f; color: #e0e0e0"
  >
    <el-form :model="form" label-width="90px" size="small" label-position="left">
      <el-form-item label="触发方式">
        <el-select v-model="form.direction" style="width: 100%">
          <el-option label="价格上穿（≥ 触发价）" value="above" />
          <el-option label="价格下穿（≤ 触发价）" value="below" />
        </el-select>
      </el-form-item>

      <el-form-item label="触发价格">
        <el-input-number
          v-model="form.trigger_price"
          :precision="2"
          :step="100"
          :min="0"
          style="width: 100%"
          placeholder="例如 80000"
        />
      </el-form-item>

      <el-form-item label="触发后执行">
        <div style="display: flex; flex-direction: column; gap: 8px; width: 100%">
          <el-checkbox v-model="actionForceClose" label="强制平仓（市价）" />
          <div style="display: flex; align-items: center; gap: 8px">
            <el-checkbox v-model="actionPause" label="暂停策略" />
            <span v-if="actionPause" style="color: #b0b0b0; font-size: 12px">
              暂停
              <el-input-number
                v-model="form.pause_hours"
                :min="1"
                :max="720"
                size="small"
                style="width: 80px"
              />
              小时
            </span>
          </div>
        </div>
      </el-form-item>

      <el-form-item label="备注">
        <el-input
          v-model="form.note"
          placeholder="（可选）事后复盘用"
          maxlength="100"
          show-word-limit
        />
      </el-form-item>
    </el-form>

    <!-- 预览 -->
    <div
      v-if="previewText"
      style="
        margin-top: 4px;
        padding: 8px 12px;
        background: #252526;
        border-radius: 4px;
        font-size: 12px;
        color: #b0b0b0;
        border-left: 3px solid #409eff;
      "
    >
      {{ previewText }}
    </div>

    <template #footer>
      <el-button size="small" @click="visible = false">取消</el-button>
      <el-button
        type="primary"
        size="small"
        :disabled="!isValid"
        :loading="submitting"
        @click="handleSubmit"
      >
        确认设置
      </el-button>
    </template>
  </el-dialog>

  <!-- 立即平仓确认弹窗 -->
  <el-dialog
    v-model="forceCloseVisible"
    title="确认强制平仓"
    width="360px"
    style="background: #1d1e1f; color: #e0e0e0"
  >
    <div style="color: #e0e0e0; line-height: 1.6">
      <p>
        将对 <strong>{{ symbol }}</strong> 执行市价平仓，预计滑点约 0.05%。
      </p>
      <el-input
        v-model="forceCloseNote"
        placeholder="备注原因（可选）"
        size="small"
        style="margin-top: 8px"
      />
    </div>
    <template #footer>
      <el-button size="small" @click="forceCloseVisible = false">取消</el-button>
      <el-button
        type="danger"
        size="small"
        :loading="forceClosing"
        @click="handleForceClose"
      >
        确认平仓
      </el-button>
    </template>
  </el-dialog>

  <!-- 暂停策略确认弹窗 -->
  <el-dialog
    v-model="pauseVisible"
    title="暂停策略"
    width="360px"
    style="background: #1d1e1f; color: #e0e0e0"
  >
    <div style="color: #e0e0e0; line-height: 1.6">
      <p>暂停策略开新仓，不影响现有持仓。</p>
      <div style="display: flex; align-items: center; gap: 8px; margin-top: 8px">
        <span>暂停时长：</span>
        <el-input-number v-model="pauseHours" :min="1" :max="720" size="small" style="width: 100px" />
        <span>小时</span>
      </div>
    </div>
    <template #footer>
      <el-button size="small" @click="pauseVisible = false">取消</el-button>
      <el-button
        type="warning"
        size="small"
        :loading="pausing"
        @click="handlePause"
      >
        确认暂停
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import {
  createOverride,
  forceClosePosition,
  pauseStrategy,
} from '@/api/override'
import type { CreateOverrideRequest, TriggerDir } from '@/types/models'

const props = defineProps<{
  symbol: string
}>()

const emit = defineEmits<{
  (e: 'created'): void
  (e: 'forceClosed'): void
  (e: 'paused'): void
}>()

// ---- 条件触发弹窗 ----
const visible = ref(false)
const submitting = ref(false)

const form = ref<CreateOverrideRequest>({
  symbol: props.symbol,
  trigger_price: 0,
  direction: 'above' as TriggerDir,
  actions: [],
  pause_hours: 24,
  note: '',
})

const actionForceClose = ref(false)
const actionPause = ref(false)

const isValid = computed(() => {
  return (
    form.value.trigger_price > 0 &&
    (actionForceClose.value || actionPause.value)
  )
})

const previewText = computed(() => {
  if (!form.value.trigger_price) return ''
  const dirLabel = form.value.direction === 'above' ? '≥' : '≤'
  const actions: string[] = []
  if (actionForceClose.value) actions.push('市价平仓')
  if (actionPause.value) actions.push(`暂停策略 ${form.value.pause_hours}h`)
  if (!actions.length) return ''
  return `当 ${props.symbol} 价格 ${dirLabel} $${form.value.trigger_price.toLocaleString()} 时 → ${actions.join(' + ')}`
})

function open() {
  form.value = {
    symbol: props.symbol,
    trigger_price: 0,
    direction: 'above',
    actions: [],
    pause_hours: 24,
    note: '',
  }
  actionForceClose.value = false
  actionPause.value = false
  visible.value = true
}

async function handleSubmit() {
  if (!isValid.value) return
  const actions = []
  if (actionForceClose.value) actions.push('force_close')
  if (actionPause.value) actions.push('pause_strategy')

  submitting.value = true
  try {
    await createOverride({ ...form.value, actions: actions as any[] })
    ElMessage.success('条件触发已设置')
    visible.value = false
    emit('created')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '设置失败')
  } finally {
    submitting.value = false
  }
}

// ---- 立即平仓弹窗 ----
const forceCloseVisible = ref(false)
const forceClosing = ref(false)
const forceCloseNote = ref('')

function openForceClose() {
  forceCloseNote.value = ''
  forceCloseVisible.value = true
}

async function handleForceClose() {
  forceClosing.value = true
  try {
    await forceClosePosition(props.symbol, forceCloseNote.value || '手动平仓')
    ElMessage.success('平仓指令已发送')
    forceCloseVisible.value = false
    emit('forceClosed')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '平仓失败')
  } finally {
    forceClosing.value = false
  }
}

// ---- 暂停策略弹窗 ----
const pauseVisible = ref(false)
const pausing = ref(false)
const pauseHours = ref(24)

function openPause() {
  pauseHours.value = 24
  pauseVisible.value = true
}

async function handlePause() {
  pausing.value = true
  try {
    await pauseStrategy(pauseHours.value)
    ElMessage.success(`策略已暂停 ${pauseHours.value}h`)
    pauseVisible.value = false
    emit('paused')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '暂停失败')
  } finally {
    pausing.value = false
  }
}

// 暴露给父组件调用
defineExpose({ open, openForceClose, openPause })
</script>
