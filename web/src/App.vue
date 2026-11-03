<script setup>
import {onMounted, reactive, ref, watch} from 'vue'
import {useRoute, useRouter} from 'vue-router'
import {adminApi, api} from './api'
import {engineReasonText} from './errors'

const GITHUB_URL = 'https://github.com/LinuxSuRen/face-recognition-server'

const health = reactive({loaded: false, engineReady: false, reason: '', version: ''})
const bannerDismissed = ref(false)
const account = reactive({checked: false, authenticated: false, username: '', role: ''})

const route = useRoute()
const router = useRouter()

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

async function loadAccount() {
  try {
    const data = await adminApi.session()
    account.checked = true
    account.authenticated = Boolean(data.authenticated)
    const user = data.user
    if (user) {
      account.username = user.username
      account.role = user.role
    }
    if (data.auth_enabled !== false && !data.authenticated && route.path !== '/login') {
      router.replace('/login')
    }
  } catch (error) {
    account.checked = true
  }
}

async function logout() {
  try {
    await adminApi.logout()
  } finally {
    window.location.href = '/login'
  }
}

onMounted(() => {
  loadHealth()
  loadAccount()
})

watch(() => route.path, () => {
  if (account.checked && !account.authenticated && route.path !== '/login') {
    // refresh the session; the server stays the source of truth via 401
    loadAccount()
  }
})
</script>

<template>
  <header class="topbar">
    <span class="brand">
      <img src="/logo.svg" alt="" class="brand-logo" />
      人脸识别服务
    </span>
    <nav v-if="account.authenticated">
      <router-link to="/faces">人员与人脸</router-link>
      <router-link to="/live">实时识别</router-link>
      <router-link v-if="account.role === 'admin'" to="/admin">系统管理</router-link>
    </nav>
    <span class="topbar-right">
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
      <span v-if="account.authenticated" class="account">
        {{ account.username }}
        <button class="link" @click="logout">退出</button>
      </span>
      <a
        class="gh"
        :href="GITHUB_URL"
        target="_blank"
        rel="noopener noreferrer"
        aria-label="GitHub 仓库"
        title="GitHub 仓库"
      >
        <svg viewBox="0 0 16 16" width="20" height="20" aria-hidden="true">
          <path fill="currentColor" d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z"/>
        </svg>
      </a>
    </span>
  </header>
  <div v-if="health.loaded && !health.engineReady && !bannerDismissed && route.path !== '/login'" class="engine-banner">
    <span>{{ engineReasonText(health.reason) }}</span>
    <button class="dismiss" aria-label="关闭提示" @click="bannerDismissed = true">×</button>
  </div>
  <main class="page">
    <router-view />
  </main>
</template>

<style scoped>
.brand {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
  font-size: 17px;
}

.brand-logo {
  width: 26px;
  height: 26px;
  border-radius: 6px;
}

.topbar-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 12px;
}

.account {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--muted);
}

.account .link {
  border: none;
  background: none;
  padding: 0;
  font-size: 13px;
  color: var(--primary);
  cursor: pointer;
}

.gh {
  display: inline-flex;
  color: var(--muted);
}

.gh:hover {
  color: var(--text);
}

.engine-chip {
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
