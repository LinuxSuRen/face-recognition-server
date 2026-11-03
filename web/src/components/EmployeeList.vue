<script setup>
defineProps({
  employees: {type: Array, default: () => []},
  selectedId: {type: Number, default: null},
  total: {type: Number, default: 0},
  page: {type: Number, default: 1},
  pageSize: {type: Number, default: 10},
  loading: {type: Boolean, default: false},
})

const emit = defineEmits(['select', 'create', 'edit', 'delete', 'page-change'])
</script>

<template>
  <div class="employee-list">
    <div class="list-toolbar">
      <button class="primary" @click="emit('create')">＋ 新增员工</button>
      <span class="muted">共 {{ total }} 人</span>
    </div>
    <p v-if="loading && !employees.length" class="muted">正在加载员工…</p>
    <p v-else-if="!employees.length" class="muted empty">还没有员工，点击“新增员工”开始。</p>
    <ul v-else class="people">
      <li
        v-for="employee in employees"
        :key="employee.id"
        :class="{active: employee.id === selectedId, disabled: !employee.enabled}"
        @click="emit('select', employee)"
      >
        <div class="person-main">
          <strong>{{ employee.name }}</strong>
          <span class="muted">{{ employee.employee_no }}</span>
        </div>
        <div class="person-side">
          <span class="count">{{ employee.sample_count }} 张</span>
          <span v-if="!employee.enabled" class="tag">已停用</span>
          <span class="actions">
            <button class="link" title="编辑" @click.stop="emit('edit', employee)">编辑</button>
            <button class="link danger" title="删除" @click.stop="emit('delete', employee)">删除</button>
          </span>
        </div>
      </li>
    </ul>
    <div v-if="total > pageSize" class="pager">
      <button :disabled="page <= 1" @click="emit('page-change', page - 1)">上一页</button>
      <span class="muted">{{ page }} / {{ Math.ceil(total / pageSize) }}</span>
      <button :disabled="page * pageSize >= total" @click="emit('page-change', page + 1)">下一页</button>
    </div>
  </div>
</template>

<style scoped>
.list-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.people {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.people li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  cursor: pointer;
  background: #fff;
}

.people li:hover {
  border-color: var(--primary);
}

.people li.active {
  border-color: var(--primary);
  background: rgba(37, 99, 235, 0.06);
}

.people li.disabled .person-main strong {
  color: var(--muted);
}

.person-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.person-side {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.count {
  font-size: 12px;
  color: var(--muted);
}

.tag {
  font-size: 12px;
  color: #b45309;
  background: rgba(245, 158, 11, 0.12);
  border-radius: 6px;
  padding: 2px 6px;
}

button.link {
  border: none;
  background: none;
  padding: 2px 4px;
  font-size: 13px;
  color: var(--primary);
}

button.link.danger {
  color: var(--danger);
}

.pager {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 12px;
}

.empty {
  padding: 18px 0;
}
</style>
