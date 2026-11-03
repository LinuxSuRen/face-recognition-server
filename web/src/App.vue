<script setup>
import {onMounted, reactive, ref} from 'vue'
import {api} from './api'
import {engineReasonText} from './errors'

const health = reactive({loaded: false, engineReady: false, reason: '', version: ''})
const bannerDismissed = ref(false)

async function loadHealth() {
  try {
    const data = await api.request('/healthz')
    health.engineReady = Boolean(data.engine && data.engine.ready)
    health.reason = (data.engine && data.engine.reason) || ''
    health.version = data.version || ''
    health.loaded = true
  } catch (error) {
    health.loaded = false
  }
}

onMounted(loadHealth)
</script>

<template>
  <header class="topbar">
    <span class="brand">人脸识别服务</span>
    <nav>
      <router-link to="/faces">人员与人脸</router-link>
      <router-link to="/live">实时识别</router-link>
    </nav>
    <span
      v-if="health.loaded"
      class="engine-chip"
      :class="health.engineReady ? 'ready' : 'degraded'"
      :title="health.engineReady ? '识别引擎已就绪' : '识别引擎未加载'"
      @click="bannerDismissed = false"
    >
      <i class="dot"></i>
      {{ health.engineReady ? '引擎就绪' : '引擎未加载' }}
    </span>
  </header>
  <div v-if="health.loaded && !health.engineReady && !bannerDismissed" class="engine-banner">
    <span>{{ engineReasonText(health.reason) }}</span>
    <button class="dismiss" aria-label="关闭提示" @click="bannerDismissed = true">×</button>
  </div>
  <main class="page">
    <router-view />
  </main>
</template>

<style scoped>
.engine-chip {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  padding: 4px 10px;
  border-radius: 999px;
  cursor: pointer;
  user-select: none;
}

.engine-chip .dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.engine-chip.ready {
  color: #047857;
  background: rgba(16, 185, 129, 0.1);
}

.engine-chip.ready .dot {
  background: var(--ok);
}

.engine-chip.degraded {
  color: #b45309;
  background: rgba(245, 158, 11, 0.12);
}

.engine-chip.degraded .dot {
  background: #f59e0b;
}

.engine-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  max-width: 1200px;
  margin: 12px auto -12px;
  padding: 10px 16px;
  border-radius: 8px;
  background: rgba(245, 158, 11, 0.1);
  color: #92400e;
  font-size: 14px;
}

.engine-banner .dismiss {
  border: none;
  background: none;
  color: #92400e;
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
  padding: 0 4px;
}
</style>
