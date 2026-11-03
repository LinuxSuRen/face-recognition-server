<script setup>
import {ref} from 'vue'
import {facesApi} from '../api'
import {faceReasonText, formatScore, friendlyError} from '../errors'

const file = ref(null)
const previewUrl = ref('')
const faces = ref([])
const summary = ref(null)
const running = ref(false)
const error = ref('')

async function onFileChange(event) {
  const selected = event.target.files && event.target.files[0]
  event.target.value = ''
  if (!selected) return
  file.value = selected
  previewUrl.value = URL.createObjectURL(selected)
  faces.value = []
  summary.value = null
  error.value = ''
  running.value = true
  try {
    const result = await facesApi.recognize(selected)
    faces.value = result.faces || []
    summary.value = result.summary || null
  } catch (err) {
    error.value = friendlyError(err)
  } finally {
    running.value = false
  }
}

function statusTitle(face) {
  if (face.recognition_status === 'known') return face.name || '员工'
  if (face.recognition_status === 'unknown') return '陌生人'
  return '无法辨认'
}

function statusClass(face) {
  return face.recognition_status
}

function drawPreview(event) {
  const image = event.target
  const canvas = document.getElementById('recognition-canvas')
  if (!canvas) return
  canvas.width = image.naturalWidth
  canvas.height = image.naturalHeight
  const context = canvas.getContext('2d')
  context.drawImage(image, 0, 0)
  for (const face of faces.value) {
    const [x1, y1, x2, y2] = face.bbox_xyxy || []
    if ([x1, y1, x2, y2].some((value) => typeof value !== 'number')) continue
    const known = face.recognition_status === 'known'
    const color = known ? '#10b981' : face.recognition_status === 'unusable' ? '#64748b' : '#ef4444'
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
    const metrics = context.measureText(label)
    const height = fontSize + padding * 2
    const top = Math.max(0, y1 - height)
    context.fillStyle = color
    context.fillRect(x1, top, metrics.width + padding * 2, height)
    context.fillStyle = '#fff'
    context.fillText(label, x1 + padding, top + fontSize + padding - 2)
  }
}
</script>

<template>
  <div class="recognition">
    <header class="recognition-header">
      <h3>识别测试</h3>
      <label class="primary pick">
        {{ running ? '正在识别…' : '选择图片测试' }}
        <input type="file" accept="image/jpeg,image/png,image/webp" hidden @change="onFileChange" />
      </label>
    </header>
    <p class="muted hint">上传一张含人脸的照片，服务会框出每张人脸并给出员工或陌生人判定。</p>
    <p v-if="error" class="error">{{ error }}</p>
    <p v-else-if="summary && !faces.length" class="muted">未检测到人脸。</p>
    <div class="recognition-body">
      <div v-if="previewUrl" class="preview">
        <img :src="previewUrl" alt="待识别图片" @load="drawPreview" />
        <canvas id="recognition-canvas"></canvas>
      </div>
      <ul v-if="faces.length" class="results">
        <li v-for="(face, index) in faces" :key="index" :class="statusClass(face)">
          <span class="dot"></span>
          <div class="result-copy">
            <strong>{{ statusTitle(face) }}</strong>
            <small>{{ faceReasonText(face) }}</small>
          </div>
          <span class="score">{{ face.recognition_status === 'unusable' ? '—' : formatScore(face.similarity) }}</span>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.recognition-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.recognition-header h3 {
  margin: 0;
}

label.pick {
  display: inline-block;
  padding: 8px 16px;
  border-radius: 8px;
  background: var(--primary);
  color: #fff;
  font-size: 14px;
  cursor: pointer;
}

.hint {
  margin: 8px 0 14px;
}

.error {
  color: var(--danger);
  background: rgba(239, 68, 68, 0.08);
  border-radius: 8px;
  padding: 10px 12px;
}

.recognition-body {
  display: grid;
  grid-template-columns: minmax(280px, 1.4fr) minmax(220px, 1fr);
  gap: 16px;
}

.preview {
  position: relative;
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow: hidden;
  background: #101827;
}

.preview img {
  width: 100%;
  display: block;
  position: static;
  z-index: -1;
}

.preview canvas {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
}

.results {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.results li {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
}

.results .dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--muted);
  flex-shrink: 0;
}

.results li.known .dot {
  background: var(--ok);
}

.results li.unknown .dot {
  background: var(--danger);
}

.result-copy {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
}

.result-copy small {
  color: var(--muted);
}

.score {
  font-variant-numeric: tabular-nums;
  color: var(--muted);
}

@media (max-width: 900px) {
  .recognition-body {
    grid-template-columns: 1fr;
  }
}
</style>
