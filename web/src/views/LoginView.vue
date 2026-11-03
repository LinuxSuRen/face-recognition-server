<script setup>
import {nextTick, onMounted, onUnmounted, reactive, ref} from 'vue'
import {api, meApi} from '../api'
import {friendlyError} from '../errors'

const tab = ref('password') // password | face
const form = reactive({username: '', password: ''})
const busy = ref(false)
const error = ref('')
const hint = ref('')
const session = reactive({authEnabled: true, oidcConfigured: false})

const face = reactive({starting: false, running: false, reason: ''})
const cameraVideo = ref(null)
let cameraStream = null
let faceTimer = null
let faceBusy = false

onMounted(async () => {
  try {
    const data = await api.request('/api/v1/auth/session')
    session.authEnabled = data.auth_enabled !== false
    session.oidcConfigured = Boolean(data.oidc_configured)
  } catch {
    // ignore: stay on the login form
  }
})

onUnmounted(stopFaceLogin)

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

function switchTab(name) {
  tab.value = name
  error.value = ''
  hint.value = ''
  if (name === 'face') {
    startFaceLogin()
  } else {
    stopFaceLogin()
  }
}

async function startFaceLogin() {
  if (face.running || face.starting) return
  face.starting = true
  face.reason = ''
  try {
    cameraStream = await navigator.mediaDevices.getUserMedia({
      video: {width: {ideal: 1280}, height: {ideal: 720}},
      audio: false,
    })
  } catch {
    face.starting = false
    face.reason = '无法访问摄像头，请改用密码登录'
    return
  }
  face.running = true
  face.starting = false
  await nextTick()
  const element = cameraVideo.value
  if (element) {
    element.srcObject = cameraStream
    try {
      await element.play()
    } catch {
      // muted + playsinline should prevent this
    }
  }
  hint.value = '正在通过人脸识别登录…'
  faceTimer = setInterval(attemptFaceLogin, 2000)
}

function stopFaceLogin() {
  if (faceTimer) {
    clearInterval(faceTimer)
    faceTimer = null
  }
  if (cameraStream) {
    cameraStream.getTracks().forEach((track) => track.stop())
    cameraStream = null
  }
  face.running = false
}

async function attemptFaceLogin() {
  const element = cameraVideo.value
  if (!element || !element.videoWidth || faceBusy) return
  faceBusy = true
  const canvas = document.createElement('canvas')
  canvas.width = element.videoWidth
  canvas.height = element.videoHeight
  canvas.getContext('2d').drawImage(element, 0, 0)
  canvas.toBlob(async (blob) => {
    try {
      if (!blob) return
      await meApi.faceLogin(new File([blob], `login-${Date.now()}.jpg`, {type: 'image/jpeg'}))
      stopFaceLogin()
      window.location.href = '/'
    } catch (err) {
      hint.value = friendlyError(err)
    } finally {
      faceBusy = false
    }
  }, 'image/jpeg', 0.85)
}
</script>

<template>
  <div class="login-wrap">
    <form class="panel login" @submit.prevent="tab === 'password' ? submit() : null">
      <img src="/logo.svg" alt="logo" class="logo" />
      <h2>人脸识别服务</h2>
      <div class="tabs" role="tablist">
        <button type="button" :class="{active: tab === 'password'}" role="tab" @click="switchTab('password')">密码登录</button>
        <button type="button" :class="{active: tab === 'face'}" role="tab" @click="switchTab('face')">人脸登录</button>
      </div>

      <template v-if="tab === 'password'">
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
      </template>

      <template v-else>
        <div class="camera">
          <video v-if="face.running" ref="cameraVideo" playsinline muted></video>
          <div v-else class="camera-off muted">
            {{ face.starting ? '正在打开摄像头…' : (face.reason || '摄像头未启动') }}
          </div>
        </div>
        <p v-if="hint || face.reason" class="hint">{{ face.reason || hint }}</p>
        <p class="muted small">未绑定人脸的账号需先在登录后前往“我的人脸”录入。</p>
      </template>
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
  width: min(380px, calc(100vw - 32px));
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

.tabs {
  display: flex;
  gap: 8px;
  margin: 10px 0 4px;
}

.tabs button {
  padding: 6px 16px;
  border-radius: 999px;
  border: 1px solid var(--line);
  background: var(--bg);
  color: var(--muted);
  font-size: 14px;
}

.tabs button.active {
  background: rgba(37, 99, 235, 0.1);
  border-color: var(--primary);
  color: var(--primary);
  font-weight: 600;
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

.camera {
  width: 100%;
  aspect-ratio: 4 / 3;
  border-radius: 8px;
  overflow: hidden;
  background: #101827;
  margin-top: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.camera video {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.hint {
  font-size: 13px;
  color: var(--muted);
  margin: 8px 0 0;
}

.small {
  font-size: 12px;
  margin-top: 4px;
}

.sso {
  margin-top: 12px;
  font-size: 14px;
  color: var(--primary);
  text-decoration: none;
}
</style>
