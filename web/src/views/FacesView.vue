<script setup>
import {onMounted, reactive, ref} from 'vue'
import {facesApi} from '../api'
import {friendlyError} from '../errors'
import EmployeeList from '../components/EmployeeList.vue'
import EmployeeDialog from '../components/EmployeeDialog.vue'
import SamplePanel from '../components/SamplePanel.vue'
import RecognitionPanel from '../components/RecognitionPanel.vue'

const state = reactive({
  employees: [],
  total: 0,
  page: 1,
  pageSize: 10,
  loading: false,
  includeDisabled: true,
})

const selected = ref(null)
const samples = ref([])
const samplesLoading = ref(false)
const uploading = ref(false)
const dialogOpen = ref(false)
const dialogEmployee = ref(null)
const notice = reactive({text: '', kind: 'info'})
const uploadReport = ref([])

function setNotice(text, kind = 'info') {
  notice.text = text
  notice.kind = kind
  if (text) {
    setTimeout(() => {
      if (notice.text === text) notice.text = ''
    }, 5000)
  }
}

async function loadEmployees(page = state.page) {
  state.loading = true
  try {
    const result = await facesApi.listEmployees({
      page,
      page_size: state.pageSize,
      include_disabled: state.includeDisabled,
    })
    state.employees = result.data || []
    state.total = result.total || 0
    state.page = result.page || page
    if (selected.value && !state.employees.some((item) => item.id === selected.value.id)) {
      // keep the selection even if it moved off-page; the detail reloads
      // through its own endpoints
    }
  } catch (error) {
    setNotice(friendlyError(error), 'error')
  } finally {
    state.loading = false
  }
}

async function selectEmployee(employee) {
  selected.value = employee
  samples.value = []
  uploadReport.value = []
  await loadSamples(employee.id)
}

async function loadSamples(employeeId) {
  samplesLoading.value = true
  try {
    const result = await facesApi.listSamples(employeeId)
    samples.value = result.data || []
  } catch (error) {
    setNotice(friendlyError(error), 'error')
  } finally {
    samplesLoading.value = false
  }
}

function openCreate() {
  dialogEmployee.value = null
  dialogOpen.value = true
}

function openEdit(employee) {
  dialogEmployee.value = employee
  dialogOpen.value = true
}

async function submitDialog(body) {
  try {
    if (dialogEmployee.value) {
      const updated = await facesApi.updateEmployee(dialogEmployee.value.id, body)
      setNotice(`已保存 ${body.name} 的信息`)
      if (selected.value && selected.value.id === updated.id) {
        selected.value = {...selected.value, ...updated}
      }
    } else {
      const created = await facesApi.createEmployee(body)
      setNotice(`已创建员工 ${body.name}`)
      state.includeDisabled = true
      await loadEmployees(1)
      await selectEmployee(created)
    }
    dialogOpen.value = false
  } catch (error) {
    setNotice(friendlyError(error), 'error')
  }
}

async function removeEmployee(employee) {
  if (!window.confirm(`确定删除“${employee.name}”及其全部 ${employee.sample_count} 张人脸样本？此操作不可撤销。`)) {
    return
  }
  try {
    await facesApi.deleteEmployee(employee.id)
    setNotice(`已删除员工 ${employee.name}`)
    if (selected.value && selected.value.id === employee.id) {
      selected.value = null
      samples.value = []
    }
    await loadEmployees()
  } catch (error) {
    setNotice(friendlyError(error), 'error')
  }
}

async function uploadSamples(files) {
  if (!selected.value) return
  uploading.value = true
  uploadReport.value = []
  const outcomes = []
  for (const file of files) {
    try {
      await facesApi.uploadSample(selected.value.id, file)
      outcomes.push({name: file.name, ok: true})
    } catch (error) {
      outcomes.push({name: file.name, ok: false, error: friendlyError(error)})
    }
  }
  uploadReport.value = outcomes
  const failed = outcomes.filter((item) => !item.ok).length
  setNotice(
    failed
      ? `上传完成：${outcomes.length - failed} 张成功，${failed} 张未通过，详见下方明细`
      : `${outcomes.length} 张人脸样本录入成功`,
    failed ? 'error' : 'info',
  )
  uploading.value = false
  await Promise.all([loadSamples(selected.value.id), loadEmployees()])
  uploadReport.value = outcomes
}

async function removeSample(sample) {
  if (!window.confirm('确定删除这张人脸样本？删除后将立即影响后续识别。')) return
  try {
    await facesApi.deleteSample(selected.value.id, sample.id)
    setNotice('样本已删除')
    await Promise.all([loadSamples(selected.value.id), loadEmployees()])
  } catch (error) {
    setNotice(friendlyError(error), 'error')
  }
}

onMounted(() => loadEmployees(1))
</script>

<template>
  <div class="faces-page">
    <p v-if="notice.text" :class="['notice', notice.kind]">{{ notice.text }}</p>
    <div class="layout">
      <section class="panel side">
        <EmployeeList
          :employees="state.employees"
          :selected-id="selected ? selected.id : null"
          :total="state.total"
          :page="state.page"
          :page-size="state.pageSize"
          :loading="state.loading"
          @select="selectEmployee"
          @create="openCreate"
          @edit="openEdit"
          @delete="removeEmployee"
          @page-change="(page) => loadEmployees(page)"
        />
      </section>
      <section class="panel main">
        <SamplePanel
          :employee="selected"
          :samples="samples"
          :uploading="uploading"
          @upload="uploadSamples"
          @delete-sample="removeSample"
        />
        <ul v-if="uploadReport.length" class="report">
          <li v-for="(item, index) in uploadReport" :key="index" :class="{ok: item.ok, fail: !item.ok}">
            <span>{{ item.name }}</span>
            <span>{{ item.ok ? '已录入' : item.error }}</span>
          </li>
        </ul>
      </section>
    </div>
    <section class="panel recognition-panel">
      <RecognitionPanel />
    </section>
    <EmployeeDialog
      :open="dialogOpen"
      :employee="dialogEmployee"
      @close="dialogOpen = false"
      @submit="submitDialog"
    />
  </div>
</template>

<style scoped>
.faces-page {
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

.notice.error {
  background: rgba(239, 68, 68, 0.08);
  color: var(--danger);
}

.layout {
  display: grid;
  grid-template-columns: minmax(280px, 1fr) minmax(360px, 1.6fr);
  gap: 16px;
  align-items: start;
}

.report {
  list-style: none;
  margin: 12px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.report li {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  font-size: 13px;
  padding: 6px 10px;
  border-radius: 6px;
  background: var(--bg);
}

.report li.ok span:last-child {
  color: var(--ok);
}

.report li.fail span:last-child {
  color: var(--danger);
}

@media (max-width: 900px) {
  .layout {
    grid-template-columns: 1fr;
  }
}
</style>
