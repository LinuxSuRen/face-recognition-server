<script setup>
import {nextTick, onMounted, onUnmounted, reactive, ref} from 'vue'
import {meApi} from '../api'
import {friendlyError} from '../errors'

const notice = ref('')
const faces = ref([])
const camera = reactive({running: false, starting: false, error: '', captured: 0})
const cameraVideo = ref(null)
let cameraStream = null
let captureTimer = null

function setNotice(text) {
  notice.value = text
  if (text) setTimeout(() => (notice.value === text ? (notice.value = '') : null), 5000)
}

async function load() {
  try {
    const data = await meApi.listFaces()
    faces.value = data.data || []
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

function onUpload(event) {
  const files = Array.from(event.target.files || [])
  event.target.value = ''
  if (!files.length) return
  uploadFile(files[0])
}

async function uploadFile(file) {
  try {
    await meApi.addFace(file)
    setNotice('人脸样本已绑定，现在可以在登录页使用人脸登录')
    await load()
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

async function removeFace(face) {
  if (!window.confirm('确定删除这条人脸样本？删除后该样本不再参与人脸登录。')) return
  try {
    await meApi.deleteFace(face.id)
    setNotice('样本已删除')
    await load()
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

async function startCamera() {
  if (camera.running || camera.starting) return
  camera.starting = true
  camera.error = ''
  camera.captured = 0
  try {
    cameraStream = await navigator.mediaDevices.getUserMedia({
      video: {width: {ideal: 1280}, height: {ideal: 720}},
      audio: false,
    })
  } catch {
    camera.starting = false
    camera.error = '无法访问摄像头，请改用图片上传'
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
    } catch {
      // muted + playsinline
    }
  }
  captureTimer = setInterval(capture, 2500)
}

function stopCamera() {
  if (captureTimer) {
    clearInterval(captureTimer)
    captureTimer = null
  }
  if (cameraStream) {
    cameraStream.getTracks().forEach((track) => track.stop())
    cameraStream = null
  }
  camera.running = false
}

function capture() {
  const element = cameraVideo.value
  if (!element || !element.videoWidth) return
  const canvas = document.createElement('canvas')
  canvas.width = element.videoWidth
  canvas.height = element.videoHeight
  canvas.getContext('2d').drawImage(element, 0, 0)
  canvas.toBlob((blob) => {
    if (!blob) return
    camera.captured++
    uploadFile(new File([blob], `me-${Date.now()}.jpg`, {type: 'image/jpeg'}))
  }, 'image/jpeg', 0.9)
}

onMounted(load)
onUnmounted(stopCamera)
</script>

<template>
  <div class="me">
    <p v-if="notice" class="notice">{{ notice }}</p>
    <section class="panel">
      <header class="row">
        <div>
          <h3>我的人脸</h3>
          <p class="muted">绑定人脸后，登录页可直接刷脸进入平台。建议录入 3～5 张不同姿态。</p>
        </div>
        <div class="actions">
          <label class="primary upload">
            上传人脸照片
            <input type="file" accept="image/jpeg,image/png,image/webp" hidden @change="onUpload" />
          </label>
          <button v-if="!camera.running" @click="startCamera">摄像头录入</button>
          <button v-else class="danger" @click="stopCamera">停止（已采 {{ camera.captured }} 张）</button>
        </div>
      </header>
      <div v-if="camera.running || camera.error" class="camera">
        <video v-if="camera.running" ref="cameraVideo" playsinline muted></video>
        <p v-if="camera.error" class="muted">{{ camera.error }}</p>
      </div>
      <p v-if="!faces.length" class="muted empty">还没有绑定的人脸。</p>
      <div v-else class="samples">
        <div v-for="face in faces" :key="face.id" class="sample">
          <strong>样本 {{ face.id }}</strong>
          <span class="muted">
            质量 {{ face.quality_score ? Math.round(face.quality_score * 100) + '%' : '—' }}
            · {{ face.created_at }}
          </span>
          <button class="link danger" @click="removeFace(face)">删除</button>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.me {
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

.row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.row h3 {
  margin: 0 0 4px;
}

.actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
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

.camera {
  margin-top: 12px;
  border-radius: 8px;
  overflow: hidden;
  background: #101827;
}

.camera video {
  width: 100%;
  max-height: 280px;
  object-fit: contain;
  display: block;
}

.samples {
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sample {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  font-size: 14px;
}

.sample strong {
  min-width: 64px;
}

.sample span {
  flex: 1;
  font-size: 13px;
}

.link {
  border: none;
  background: none;
  padding: 0;
  font-size: 13px;
  cursor: pointer;
  color: var(--danger);
}

.empty {
  padding: 14px 0;
  margin: 8px 0 0;
}
</style>
