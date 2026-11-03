<script setup>
import {onMounted, reactive, ref} from 'vue'
import {api} from '../api'
import {friendlyError} from '../errors'

const form = reactive({username: '', password: ''})
const busy = ref(false)
const error = ref('')
const session = reactive({authEnabled: true, oidcConfigured: false})

onMounted(async () => {
  try {
    const data = await api.request('/api/v1/auth/session')
    session.authEnabled = data.auth_enabled !== false
    session.oidcConfigured = Boolean(data.oidc_configured)
    if (!session.authEnabled) {
      error.value = ''
    }
  } catch {
    // ignore: stay on the login form
  }
})

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.request('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({username: form.username.trim(), password: form.password}),
    })
    window.location.href = '/'
  } catch (err) {
    error.value = friendlyError(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <form class="panel login" @submit.prevent="submit">
      <img src="/logo.svg" alt="logo" class="logo" />
      <h2>人脸识别服务</h2>
      <p class="muted">请使用平台账号登录</p>
      <div class="field">
        <label for="username">用户名</label>
        <input id="username" v-model="form.username" autocomplete="username" spellcheck="false" />
      </div>
      <div class="field">
        <label for="password">密码</label>
        <input id="password" v-model="form.password" type="password" autocomplete="current-password" />
      </div>
      <p v-if="error" class="error">{{ error }}</p>
      <button class="primary block" type="submit" :disabled="busy">
        {{ busy ? '正在登录…' : '登录' }}
      </button>
      <a v-if="session.oidcConfigured" class="sso" href="/api/v1/auth/oauth/start">
        使用 SSO 登录
      </a>
    </form>
  </div>
</template>

<style scoped>
.login-wrap {
  min-height: 70vh;
  display: flex;
  align-items: center;
  justify-content: center;
}

.login {
  width: min(360px, calc(100vw - 32px));
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
}

.logo {
  width: 56px;
  height: 56px;
}

.login h2 {
  margin: 8px 0 0;
}

.field {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 10px 0 0;
}

.field label {
  font-size: 13px;
  color: var(--muted);
}

.field input {
  width: 100%;
}

.block {
  width: 100%;
  margin-top: 16px;
}

.error {
  width: 100%;
  color: var(--danger);
  background: rgba(239, 68, 68, 0.08);
  border-radius: 8px;
  padding: 8px 12px;
  font-size: 14px;
  margin: 8px 0 0;
}

.sso {
  margin-top: 12px;
  font-size: 14px;
  color: var(--primary);
  text-decoration: none;
}
</style>
