<script setup>
import {reactive, watch} from 'vue'

const props = defineProps({
  open: {type: Boolean, default: false},
  employee: {type: Object, default: null},
})

const emit = defineEmits(['close', 'submit'])

const form = reactive({employee_no: '', name: '', department: '', enabled: true})

watch(
  () => props.open,
  (open) => {
    if (!open) return
    form.employee_no = props.employee ? props.employee.employee_no : ''
    form.name = props.employee ? props.employee.name : ''
    form.department = props.employee && props.employee.department ? props.employee.department : ''
    form.enabled = props.employee ? props.employee.enabled : true
  },
)

function submit() {
  const body = {
    employee_no: form.employee_no.trim(),
    name: form.name.trim(),
    department: form.department.trim(),
    enabled: form.enabled,
  }
  emit('submit', body)
}
</script>

<template>
  <div v-if="open" class="mask" @click.self="emit('close')">
    <div class="dialog panel">
      <h3>{{ employee ? '编辑员工' : '新增员工' }}</h3>
      <div class="field">
        <label for="employee-no">工号</label>
        <input id="employee-no" v-model="form.employee_no" maxlength="100" placeholder="例如 E001" />
      </div>
      <div class="field">
        <label for="employee-name">姓名</label>
        <input id="employee-name" v-model="form.name" maxlength="100" placeholder="员工姓名" />
      </div>
      <div class="field">
        <label for="employee-dept">部门</label>
        <input id="employee-dept" v-model="form.department" maxlength="200" placeholder="选填" />
      </div>
      <label class="check">
        <input v-model="form.enabled" type="checkbox" />
        启用识别
      </label>
      <div class="dialog-actions">
        <button @click="emit('close')">取消</button>
        <button class="primary" @click="submit">{{ employee ? '保存' : '创建' }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 20;
}

.dialog {
  width: min(420px, calc(100vw - 32px));
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 12px;
}

.field label {
  font-size: 13px;
  color: var(--muted);
}

.check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  margin-bottom: 16px;
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
