<script setup>
import {nextTick, onUnmounted, reactive, ref} from 'vue'

defineProps({
  employee: {type: Object, default: null},
  samples: {type: Array, default: () => []},
  uploading: {type: Boolean, default: false},
})

const emit = defineEmits(['upload', 'delete-sample'])

const camera = reactive({
  // open: the panel is visible; active: a stream is running
  open: false,
  active: false,
  error: '',
  captured: 0,
  devices: [],
  deviceId: '',
})
const cameraVideo = ref(null)
let stream = null
let timer = null

function onFileChange(event) {
  const files = Array.from(event.target.files || [])
  if (files.length) emit('upload', files)
  event.target.value = ''
}

async function openCamera() {
  camera.open = true
  camera.captured = 0
  await startStream()
}

function closeCamera() {
  camera.open = false
  stopEverything()
}

function stopEverything() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
  if (stream) {
    stream.getTracks().forEach((track) => track.stop())
    stream = null
  }
  camera.active = false
}

// startStream opens the camera, preferring the selected device; without a
// selection the browser default is used first and the dropdown reflects
// the actually opened device afterwards, so machines with several cameras
// (including virtual ones) can switch explicitly.
async function startStream(deviceId) {
  stopEverything()
  camera.error = ''
  const video = deviceId
    ? {deviceId: {exact: deviceId}, width: {ideal: 1280}, height: {ideal: 720}}
    : {width: {ideal: 1280}, height: {ideal: 720}}
  try {
    stream = await navigator.mediaDevices.getUserMedia({video, audio: false})
  } catch (error) {
    camera.error = '无法访问摄像头，请检查浏览器权限或选择其他设备'
    return
  }
  camera.active = true
  await nextTick()
  const element = cameraVideo.value
  if (element) {
    element.srcObject = stream
    try {
      await element.play()
    } catch (error) {
      // autoplay guard: muted + playsinline should prevent this
    }
  }
  await listDevices()
  if (!timer) {
    timer = setInterval(captureFrame, 2500)
  }
}

async function listDevices() {
  try {
    const devices = await navigator.mediaDevices.enumerateDevices()
    camera.devices = devices
      .filter((device) => device.kind === 'videoinput')
      .map((device, index) => ({
        deviceId: device.deviceId,
        label: device.label || `摄像头 ${index + 1}`,
      }))
    const track = stream && stream.getVideoTracks()[0]
    const settings = track && track.getSettings ? track.getSettings() : null
    if (settings && settings.deviceId) {
      camera.deviceId = settings.deviceId
    }
  } catch (error) {
    // device labels stay empty before permission; not fatal
  }
}

function switchCamera() {
  if (camera.deviceId) {
    startStream(camera.deviceId)
  }
}

function captureFrame() {
  const element = cameraVideo.value
  if (!element || !element.videoWidth || !stream) return
  const canvas = document.createElement('canvas')
  canvas.width = element.videoWidth
  canvas.height = element.videoHeight
  canvas.getContext('2d').drawImage(element, 0, 0)
  canvas.toBlob((blob) => {
    if (!blob) return
    camera.captured++
    emit('upload', [new File([blob], `camera-${Date.now()}.jpg`, {type: 'image/jpeg'})])
  }, 'image/jpeg', 0.9)
}

onUnmounted(stopEverything)
</script>

<template>
  <div class="sample-panel">
    <template v-if="employee">
      <header class="sample-header">
        <div>
          <h3>{{ employee.name }}</h3>
          <p class="muted">工号 {{ employee.employee_no }}<template v-if="employee.department"> · {{ employee.department }}</template></p>
        </div>
        <div class="sample-actions">
          <label class="upload primary" :class="{busy: uploading}">
            {{ uploading ? '正在录入…' : '上传人脸照片' }}
            <input type="file" accept="image/jpeg,image/png,image/webp" multiple hidden @change="onFileChange" />
          </label>
          <button v-if="!camera.open" @click="openCamera">摄像头录入</button>
          <button v-else class="danger" @click="closeCamera">
            停止采集（已拍 {{ camera.captured }} 张）
          </button>
        </div>
      </header>
      <p class="muted hint">支持多选。每张照片需只有本人正脸、清晰且大小适中；与已有样本不一致的照片会被自动拒绝。</p>
      <div v-if="camera.open" class="camera panel-inset">
        <video ref="cameraVideo" playsinline muted></video>
        <div class="camera-bar">
          <select v-model="camera.deviceId" aria-label="选择摄像头" :disabled="camera.devices.length < 2" @change="switchCamera">
            <option v-for="device in camera.devices" :key="device.deviceId" :value="device.deviceId">
              {{ device.label }}
            </option>
          </select>
          <span v-if="camera.active" class="muted">预览中 · 每 2.5 秒自动采集一帧</span>
        </div>
        <p v-if="camera.error" class="muted">{{ camera.error }}</p>
      </div>
      <p v-if="!samples.length" class="muted empty">该员工还没有人脸样本。</p>
      <div v-else class="samples">
        <figure v-for="sample in samples" :key="sample.id" class="sample">
          <img
            v-if="sample.has_image"
            :src="sample.image_url"
            :alt="`人脸样本 ${sample.id}`"
            loading="lazy"
          />
          <div v-else class="no-image">无预览</div>
          <figcaption>
            <span>{{ sample.quality_score ? `质量 ${Math.round(sample.quality_score * 100)}%` : `样本 ${sample.id}` }}</span>
            <button class="link danger" @click="emit('delete-sample', sample)">删除</button>
          </figcaption>
        </figure>
      </div>
    </template>
    <p v-else class="muted empty">请从左侧选择一名员工，然后录入人脸样本。</p>
  </div>
</template>

<style scoped>
.sample-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.sample-header h3 {
  margin: 0 0 4px;
}

.sample-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

label.upload {
  display: inline-block;
  padding: 8px 16px;
  border-radius: 8px;
  background: var(--primary);
  color: #fff;
  font-size: 14px;
  cursor: pointer;
  white-space: nowrap;
}

button.danger {
  border-color: var(--danger);
  color: var(--danger);
}

.hint {
  margin: 8px 0 14px;
}

.camera {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 14px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--bg);
}

.camera video {
  width: 100%;
  max-height: 280px;
  object-fit: contain;
  border-radius: 6px;
  background: #101827;
}

.camera-bar {
  display: flex;
  align-items: center;
  gap: 12px;
}

.camera-bar select {
  max-width: 260px;
}

.samples {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 12px;
}

.sample {
  margin: 0;
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow: hidden;
  background: #fff;
}

.sample img {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  display: block;
}

.no-image {
  aspect-ratio: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--muted);
  background: var(--bg);
}

figcaption {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 8px;
  font-size: 12px;
  color: var(--muted);
}

button.link {
  border: none;
  background: none;
  padding: 0;
  font-size: 12px;
  color: var(--danger);
  cursor: pointer;
}

.empty {
  padding: 18px 0;
}
</style>
