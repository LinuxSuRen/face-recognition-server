<script setup>
import {computed, nextTick, onMounted, onUnmounted, reactive, ref, watch} from 'vue'
import {facesApi, rtspApi} from '../api'
import {faceReasonText, formatScore, friendlyError} from '../errors'

// sourceMode: 'rtsp' (server-side pull + SSE) or 'camera' (browser capture
// feeding the recognize endpoint frame by frame)
const sourceMode = ref('rtsp')
const configuration = reactive({url: '', transport: 'tcp', configured: false, loaded: false})
const session = reactive({id: null, status: 'idle', detail: ''})
const faces = ref([])
const summary = ref(null)
const frameUrl = ref('')
const notice = ref('')
let source = null

const mode = ref('recognize')
const employees = ref([])
const employeeId = ref(null)
const enrollment = reactive({state: '', saved_count: 0, target_samples: 5, candidate_ready: false, hint: ''})

const enrollReady = computed(() => mode.value === 'enroll' && employeeId.value)

// ---- camera state ----
const camera = reactive({running: false, starting: false, error: '', devices: [], deviceId: '', fps: 0})
const cameraVideo = ref(null)
let cameraStream = null
let cameraTimer = null
let cameraBusy = false

function setNotice(text) {
  notice.value = text
  if (text) setTimeout(() => (notice.value === text ? (notice.value = '') : null), 5000)
}

watch(sourceMode, (value) => {
  if (value === 'camera') {
    mode.value = 'recognize' // enrollment is the RTSP flow
    stopSession()
  } else {
    stopCamera()
  }
})

async function loadConfiguration() {
  try {
    const data = await rtspApi.getConfiguration()
    configuration.loaded = true
    configuration.configured = Boolean(data.configured !== false && data.url)
    configuration.url = data.url || ''
    configuration.transport = data.transport || 'tcp'
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

async function loadEmployees() {
  try {
    const data = await facesApi.listEmployees({page: 1, page_size: 100, include_disabled: false})
    employees.value = data.data || []
    if (employees.value.length && !employeeId.value) {
      employeeId.value = employees.value[0].id
    }
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

watch(mode, (value) => {
  if (value === 'enroll' && !employees.value.length) {
    loadEmployees()
  }
})

async function saveConfiguration() {
  try {
    await rtspApi.updateConfiguration({url: configuration.url, transport: configuration.transport})
    configuration.configured = true
    setNotice('RTSP 配置已保存')
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

function closeStream() {
  if (source) {
    source.close()
    source = null
  }
}

async function startSession() {
  if (!configuration.configured) {
    setNotice('请先填写并保存 RTSP 地址')
    return
  }
  if (mode.value === 'enroll' && !employeeId.value) {
    setNotice('请选择要录入的员工')
    return
  }
  try {
    const body = mode.value === 'enroll' ? {mode: 'enroll', employee_id: employeeId.value} : {}
    const created = await rtspApi.createSession(body)
    session.id = created.session_id
    session.status = created.status || 'connecting'
    session.detail = created.detail || ''
    faces.value = []
    summary.value = null
    if (mode.value === 'enroll') {
      Object.assign(enrollment, {state: 'waiting_for_face', saved_count: 0, target_samples: 5, candidate_ready: false, hint: ''})
    }
    openStream()
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

function openStream() {
  closeStream()
  source = new EventSource(`/api/v1/rtsp/sessions/${session.id}/events`)
  source.addEventListener('message', (event) => {
    const data = JSON.parse(event.data)
    if (data.type === 'state') {
      session.status = data.status
      session.detail = data.detail || ''
      if (data.enrollment) {
        Object.assign(enrollment, data.enrollment)
      }
      return
    }
    if (data.type !== 'frame') return
    frameUrl.value = data.image
    faces.value = data.faces || []
    summary.value = data.summary || null
    if (data.enrollment) {
      Object.assign(enrollment, data.enrollment)
    }
  })
  source.onerror = () => {
    // EventSource auto-reconnects; terminal states surface via state events
  }
}

let confirming = false
async function confirmFirstFace() {
  if (!session.id || confirming) return
  confirming = true
  try {
    const snapshot = await rtspApi.confirmEnrollment(session.id)
    Object.assign(enrollment, snapshot)
  } catch (error) {
    setNotice(friendlyError(error))
  } finally {
    confirming = false
  }
}

async function stopSession() {
  const id = session.id
  closeStream()
  if (!id) return
  try {
    await rtspApi.stopSession(id)
  } catch (error) {
    setNotice(friendlyError(error))
  }
  session.id = null
  session.status = 'idle'
  session.detail = '会话已停止'
}

// ---- local camera flow ----

function stopCameraMedia() {
  if (cameraTimer) {
    clearInterval(cameraTimer)
    cameraTimer = null
  }
  if (cameraStream) {
    cameraStream.getTracks().forEach((track) => track.stop())
    cameraStream = null
  }
}

function stopCamera() {
  stopCameraMedia()
  camera.running = false
  camera.fps = 0
  faces.value = []
  summary.value = null
}

async function openCamera(deviceId) {
  stopCameraMedia()
  camera.error = ''
  camera.starting = true
  const video = deviceId
    ? {deviceId: {exact: deviceId}, width: {ideal: 1280}, height: {ideal: 720}}
    : {width: {ideal: 1280}, height: {ideal: 720}}
  try {
    cameraStream = await navigator.mediaDevices.getUserMedia({video, audio: false})
  } catch (error) {
    camera.starting = false
    camera.error = '无法访问摄像头，请检查浏览器权限或选择其他设备'
    return
  }
  camera.running = true
  camera.starting = false
  await nextTick()
  const element = cameraVideo.value
  if (element) {
    element.srcObject = cameraStream
    try {
      await element.play()
    } catch (error) {
      // autoplay guard: muted + playsinline should prevent this
    }
  }
  await listCameraDevices()
  faces.value = []
  summary.value = null
  cameraTimer = setInterval(recognizeCameraFrame, 800)
}

async function listCameraDevices() {
  try {
    const devices = await navigator.mediaDevices.enumerateDevices()
    camera.devices = devices
      .filter((device) => device.kind === 'videoinput')
      .map((device, index) => ({
        deviceId: device.deviceId,
        label: device.label || `摄像头 ${index + 1}`,
      }))
    const track = cameraStream && cameraStream.getVideoTracks()[0]
    const settings = track && track.getSettings ? track.getSettings() : null
    if (settings && settings.deviceId) {
      camera.deviceId = settings.deviceId
    }
  } catch (error) {
    // device labels need permission; not fatal
  }
}

function switchCamera() {
  if (camera.deviceId) {
    openCamera(camera.deviceId)
  }
}

async function recognizeCameraFrame() {
  const element = cameraVideo.value
  if (!element || !element.videoWidth || !cameraStream || cameraBusy) return
  cameraBusy = true
  const canvas = document.createElement('canvas')
  canvas.width = element.videoWidth
  canvas.height = element.videoHeight
  canvas.getContext('2d').drawImage(element, 0, 0)
  canvas.toBlob(async (blob) => {
    try {
      if (!blob) return
      const result = await facesApi.recognize(
        new File([blob], `live-${Date.now()}.jpg`, {type: 'image/jpeg'}))
      faces.value = result.faces || []
      summary.value = result.summary || null
      camera.fps = faces.value.length
      const overlay = document.getElementById('live-canvas')
      if (overlay) {
        overlay.width = element.videoWidth
        overlay.height = element.videoHeight
        drawOverlay(overlay)
      }
    } catch (error) {
      camera.error = friendlyError(error)
    } finally {
      cameraBusy = false
    }
  }, 'image/jpeg', 0.85)
}

// ---- shared rendering ----

function statusText(status) {
  const map = {
    idle: '未开始',
    connecting: '正在连接',
    live: '实时识别中',
    reconnecting: '正在重连',
    failed: '连接失败',
    expired: '会话已超时',
    stopped: '会话已停止',
  }
  return map[status] || status
}

function effectiveStatus() {
  if (sourceMode.value === 'camera') {
    return camera.running ? 'live' : 'idle'
  }
  return session.status
}

function onFrameLoad(event) {
  const image = event.target
  const canvas = document.getElementById('live-canvas')
  if (!canvas) return
  canvas.width = image.naturalWidth
  canvas.height = image.naturalHeight
  drawOverlay(canvas)
}

function drawOverlay(canvas) {
  const context = canvas.getContext('2d')
  context.clearRect(0, 0, canvas.width, canvas.height)
  const enroll = mode.value === 'enroll' && sourceMode.value === 'rtsp'
  for (const face of faces.value) {
    const [x1, y1, x2, y2] = face.bbox_xyxy || []
    if ([x1, y1, x2, y2].some((value) => typeof value !== 'number')) continue
    const known = face.recognition_status === 'known'
    const color = enroll
      ? '#38bdf8'
      : known ? '#38bdf8' : face.recognition_status === 'unusable' ? '#64748b' : '#f87171'
    context.strokeStyle = color
    context.lineWidth = Math.max(2, Math.round(canvas.width / 400))
    context.strokeRect(x1, y1, x2 - x1, y2 - y1)
    if (enroll) continue
    const label = known
      ? `${face.name} ${formatScore(face.similarity)}`
      : face.recognition_status === 'unusable'
        ? faceReasonText(face) || '无法辨认'
        : `陌生人 ${formatScore(face.similarity)}`
    const fontSize = Math.max(14, Math.round(canvas.width / 45))
    context.font = `${fontSize}px system-ui, 'PingFang SC', 'Microsoft YaHei', sans-serif`
    const padding = 4
    const width = context.measureText(label).width + padding * 2
    const height = fontSize + padding * 2
    const top = Math.max(0, y1 - height)
    context.fillStyle = color
    context.fillRect(x1, top, width, height)
    context.fillStyle = '#fff'
    context.fillText(label, x1 + padding, top + fontSize + padding - 2)
  }
}

function statusClass(face) {
  return face.recognition_status
}

function statusTitle(face) {
  if (face.recognition_status === 'known') return face.name || '员工'
  if (face.recognition_status === 'unknown') return '陌生人'
  return '无法辨认'
}

onMounted(loadConfiguration)
onUnmounted(() => {
  closeStream()
  stopCamera()
  if (session.id) rtspApi.stopSession(session.id).catch(() => {})
})
</script>

<template>
  <div class="live-page">
    <p v-if="notice" class="notice">{{ notice }}</p>

    <section class="panel config">
      <h3>视频源</h3>
      <div class="mode-row">
        <label><input v-model="sourceMode" type="radio" value="rtsp" /> RTSP 网络流</label>
        <label><input v-model="sourceMode" type="radio" value="camera" /> 本机摄像头</label>
      </div>
      <template v-if="sourceMode === 'rtsp'">
        <div class="config-row">
          <input
            v-model="configuration.url"
            placeholder="rtsp://user:pass@camera/stream"
            spellcheck="false"
          />
          <select v-model="configuration.transport" aria-label="传输协议">
            <option value="tcp">TCP（推荐）</option>
            <option value="udp">UDP</option>
          </select>
          <button class="primary" @click="saveConfiguration">保存</button>
        </div>
        <p class="muted">
          服务端通过 ffmpeg 拉流，只分析最新帧；建议使用 H.264 子码流，识别到的人脸过小时再切换主码流。
        </p>
      </template>
      <template v-else>
        <div class="config-row">
          <select
            v-model="camera.deviceId"
            aria-label="选择摄像头"
            :disabled="!camera.devices.length || !camera.running"
            @change="switchCamera"
          >
            <option v-if="!camera.devices.length" value="">启动摄像头后可选择设备</option>
            <option v-for="device in camera.devices" :key="device.deviceId" :value="device.deviceId">
              {{ device.label }}
            </option>
          </select>
        </div>
        <p class="muted">浏览器直接采集，每 0.8 秒识别一帧；多摄像头可在启动后从下拉切换。</p>
      </template>
    </section>

    <section class="panel viewer">
      <header class="viewer-header">
        <h3>实时画面</h3>
        <div class="viewer-state">
          <span class="dot" :class="effectiveStatus()"></span>
          <span v-if="sourceMode === 'rtsp'">
            {{ statusText(session.status) }}<template v-if="session.detail"> · {{ session.detail }}</template>
          </span>
          <span v-else>{{ camera.running ? '摄像头识别中' : (camera.starting ? '正在打开摄像头…' : '摄像头未启动') }}</span>
        </div>
        <div class="viewer-actions">
          <template v-if="sourceMode === 'rtsp'">
            <button v-if="!session.id" class="primary" :disabled="!configuration.configured || (mode === 'enroll' && !enrollReady)" @click="startSession">
              {{ mode === 'enroll' ? '开始录入' : '开始识别' }}
            </button>
            <button v-if="session.id && mode === 'enroll' && enrollment.candidate_ready" class="primary" @click="confirmFirstFace">
              确认并保存第 1 张
            </button>
            <button v-if="session.id" @click="stopSession">停止</button>
          </template>
          <template v-else>
            <button v-if="!camera.running" class="primary" :disabled="camera.starting" @click="openCamera()">
              {{ camera.starting ? '正在打开…' : '开始识别' }}
            </button>
            <button v-else @click="stopCamera">停止</button>
          </template>
        </div>
      </header>
      <div v-if="sourceMode === 'rtsp'" class="mode-row">
        <label><input v-model="mode" type="radio" value="recognize" /> 实时识别</label>
        <label><input v-model="mode" type="radio" value="enroll" :disabled="Boolean(session.id)" /> 员工人脸录入</label>
        <select v-if="mode === 'enroll'" v-model="employeeId" :disabled="Boolean(session.id)" aria-label="选择员工">
          <option v-if="!employees.length" :value="null">暂无启用员工，请先到“人员与人脸”页新增</option>
          <option v-for="item in employees" :key="item.id" :value="item.id">{{ item.name }}（{{ item.employee_no }}）</option>
        </select>
      </div>
      <p v-if="sourceMode === 'camera' && camera.error" class="camera-error">{{ camera.error }}</p>
      <div class="viewer-body">
        <div class="stage">
          <video
            v-if="sourceMode === 'camera' && camera.running"
            ref="cameraVideo"
            playsinline
            muted
          ></video>
          <img v-else-if="frameUrl" :src="frameUrl" alt="实时画面" @load="onFrameLoad" />
          <div v-else class="placeholder muted">
            {{ sourceMode === 'camera' ? '点击“开始识别”打开摄像头。' : (session.id ? '等待画面…' : '配置视频源后点击开始。') }}
          </div>
          <canvas id="live-canvas"></canvas>
        </div>
        <aside class="faces">
          <template v-if="sourceMode === 'rtsp' && mode === 'enroll'">
            <div class="enroll-progress">
              <strong>已保存 {{ enrollment.saved_count }} / {{ enrollment.target_samples }} 张</strong>
              <div class="progress-track">
                <div class="progress-bar" :style="{width: `${(enrollment.saved_count / enrollment.target_samples) * 100}%`}"></div>
              </div>
              <p class="muted">{{ enrollment.hint || '等待合格人脸…' }}</p>
              <p class="muted small">录入的第 1 张需人工确认；后续自动采集，间隔至少 2 秒，过于相似的画面会跳过。</p>
            </div>
          </template>
          <template v-else>
            <p class="muted" v-if="summary">
              共 {{ summary.count }} 张 · 员工 {{ summary.known }} · 陌生人 {{ summary.unknown }}
            </p>
            <ul>
              <li v-for="(face, index) in faces" :key="index" :class="statusClass(face)">
                <span class="dot"></span>
                <div class="copy">
                  <strong>{{ statusTitle(face) }}</strong>
                  <small>{{ faceReasonText(face) }}</small>
                </div>
                <span class="score">{{ face.recognition_status === 'unusable' ? '—' : formatScore(face.similarity) }}</span>
              </li>
            </ul>
          </template>
        </aside>
      </div>
    </section>
  </div>
</template>

<style scoped>
.live-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.notice {
  border-radius: 8px;
  padding: 10px 14px;
  background: rgba(37, 99, 235, 0.08);
  color: var(--primary-dark);
}

.config-row {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.config-row input {
  flex: 1;
}

.mode-row {
  display: flex;
  align-items: center;
  gap: 18px;
  margin-top: 8px;
  font-size: 14px;
}

.mode-row label {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
}

.camera-error {
  margin: 10px 0 0;
  color: var(--danger);
  background: rgba(239, 68, 68, 0.08);
  border-radius: 8px;
  padding: 8px 12px;
  font-size: 14px;
}

.viewer-header {
  display: flex;
  align-items: center;
  gap: 16px;
}

.viewer-header h3 {
  margin: 0;
}

.viewer-state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 14px;
  flex: 1;
}

.dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #94a3b8;
}

.dot.live {
  background: var(--ok);
}

.dot.connecting,
.dot.reconnecting {
  background: #f59e0b;
}

.dot.failed {
  background: var(--danger);
}

.viewer-body {
  display: grid;
  grid-template-columns: minmax(320px, 1.6fr) minmax(240px, 1fr);
  gap: 16px;
  margin-top: 12px;
}

.stage {
  position: relative;
  border-radius: 8px;
  overflow: hidden;
  background: #101827;
  aspect-ratio: 16 / 9;
}

.stage img,
.stage video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
}

.stage canvas {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
}

.placeholder {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

.faces ul {
  list-style: none;
  margin: 8px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.faces li {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
}

.faces li .dot {
  flex-shrink: 0;
}

.faces li.known .dot {
  background: #38bdf8;
}

.faces li.unknown .dot {
  background: var(--danger);
}

.faces .copy {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
}

.faces .copy small {
  color: var(--muted);
}

.faces .score {
  font-variant-numeric: tabular-nums;
  color: var(--muted);
}

.enroll-progress strong {
  display: block;
  margin-bottom: 8px;
}

.progress-track {
  height: 8px;
  border-radius: 4px;
  background: var(--bg);
  overflow: hidden;
  margin-bottom: 10px;
}

.progress-bar {
  height: 100%;
  background: var(--primary);
  transition: width 0.3s ease;
}

.small {
  font-size: 12px;
}

@media (max-width: 900px) {
  .viewer-body {
    grid-template-columns: 1fr;
  }
}
</style>
