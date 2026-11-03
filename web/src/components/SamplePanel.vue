<script setup>
defineProps({
  employee: {type: Object, default: null},
  samples: {type: Array, default: () => []},
  uploading: {type: Boolean, default: false},
})

const emit = defineEmits(['upload', 'delete-sample'])

function onFileChange(event) {
  const files = Array.from(event.target.files || [])
  if (files.length) emit('upload', files)
  event.target.value = ''
}
</script>

<template>
  <div class="sample-panel">
    <template v-if="employee">
      <header class="sample-header">
        <div>
          <h3>{{ employee.name }}</h3>
          <p class="muted">工号 {{ employee.employee_no }}<template v-if="employee.department"> · {{ employee.department }}</template></p>
        </div>
        <label class="upload primary" :class="{busy: uploading}">
          {{ uploading ? '正在录入…' : '上传人脸照片' }}
          <input type="file" accept="image/jpeg,image/png,image/webp" multiple hidden @change="onFileChange" />
        </label>
      </header>
      <p class="muted hint">支持多选。每张照片需只有本人正脸、清晰且大小适中；与已有样本不一致的照片会被自动拒绝。</p>
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

label.upload.busy {
  opacity: 0.6;
  cursor: wait;
}

.hint {
  margin: 8px 0 14px;
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
