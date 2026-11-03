<script setup>
import {onMounted, onUnmounted, reactive, ref} from 'vue'
import {rtspApi} from '../api'
import {faceReasonText, formatScore, friendlyError} from '../errors'

const configuration = reactive({url: '', transport: 'tcp', configured: false, loaded: false})
const session = reactive({id: null, status: 'idle', detail: ''})
const faces = ref([])
const summary = ref(null)
const frameUrl = ref('')
const frameSize = reactive({width: 0, height: 0})
const notice = ref('')
let source = null

function setNotice(text) {
  notice.value = text
  if (text) setTimeout(() => (notice.value === text ? (notice.value = '') : null), 5000)
}

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
  try {
    const created = await rtspApi.createSession()
    session.id = created.session_id
    session.status = created.status || 'connecting'
    session.detail = created.detail || ''
    faces.value = []
    summary.value = null
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
      return
    }
    if (data.type !== 'frame') return
    frameUrl.value = data.image
    faces.value = data.faces || []
    summary.value = data.summary || null
  })
  source.onerror = () => {
    // EventSource auto-reconnects; terminal states surface via state events
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

function onFrameLoad(event) {
  const image = event.target
  frameSize.width = image.naturalWidth
  frameSize.height = image.naturalHeight
  const canvas = document.getElementById('live-canvas')
  if (!canvas) return
  canvas.width = image.naturalWidth
  canvas.height = image.naturalHeight
  drawOverlay(canvas)
}

function drawOverlay(canvas) {
  const context = canvas.getContext('2d')
  context.clearRect(0, 0, canvas.width, canvas.height)
  for (const face of faces.value) {
    const [x1, y1, x2, y2] = face.bbox_xyxy || []
    if ([x1, y1, x2, y2].some((value) => typeof value !== 'number')) continue
    const known = face.recognition_status === 'known'
    const color = known ? '#38bdf8' : face.recognition_status === 'unusable' ? '#64748b' : '#f87171'
    context.strokeStyle = color
    context.lineWidth = Math.max(2, Math.round(canvas.width / 400))
    context.strokeRect(x1, y1, x2 - x1, y2 - y1)
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
  if (session.id) rtspApi.stopSession(session.id).catch(() => {})
})
</script>

<template>
  <div class="live-page">
    <p v-if="notice" class="notice">{{ notice }}</p>
    <section class="panel config">
      <h3>视频源</h3>
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
    </section>

    <section class="panel viewer">
      <header class="viewer-header">
        <h3>实时画面</h3>
        <div class="viewer-state">
          <span class="dot" :class="session.status"></span>
          <span>{{ statusText(session.status) }}<template v-if="session.detail"> · {{ session.detail }}</template></span>
        </div>
        <div class="viewer-actions">
          <button v-if="!session.id" class="primary" :disabled="!configuration.configured" @click="startSession">
            开始识别
          </button>
          <button v-else @click="stopSession">停止</button>
        </div>
      </header>
      <div class="viewer-body">
        <div class="stage">
          <img v-if="frameUrl" :src="frameUrl" alt="实时画面" @load="onFrameLoad" />
          <div v-else class="placeholder muted">
            {{ session.id ? '等待画面…' : '配置视频源后点击“开始识别”。' }}
          </div>
          <canvas id="live-canvas"></canvas>
        </div>
        <aside class="faces">
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
}

.config-row input {
  flex: 1;
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

.stage img {
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

@media (max-width: 900px) {
  .viewer-body {
    grid-template-columns: 1fr;
  }
}
</style>
