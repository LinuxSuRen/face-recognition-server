<script setup>
import {onMounted, reactive, ref} from 'vue'
import {adminApi} from '../api'
import {friendlyError} from '../errors'

const notice = ref('')
const users = ref([])
const userPage = reactive({page: 1, total: 0, pageSize: 20})
const tokens = ref([])

const userFormOpen = ref(false)
const userForm = reactive({username: '', display_name: '', password: '', role: 'user'})
const tokenFormOpen = ref(false)
const tokenForm = reactive({name: '', scopes: []})
const issuedToken = ref('')

function setNotice(text) {
  notice.value = text
  if (text) setTimeout(() => (notice.value === text ? (notice.value = '') : null), 6000)
}

async function loadUsers() {
  try {
    const data = await adminApi.listUsers({page: userPage.page, page_size: userPage.pageSize})
    users.value = data.data || []
    userPage.total = data.total || 0
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

async function loadTokens() {
  try {
    const data = await adminApi.listTokens()
    tokens.value = data.data || []
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

async function createUser() {
  try {
    await adminApi.createUser({
      username: userForm.username.trim(),
      display_name: userForm.display_name.trim(),
      password: userForm.password,
      role: userForm.role,
    })
    setNotice(`用户 ${userForm.username} 已创建`)
    userFormOpen.value = false
    Object.assign(userForm, {username: '', display_name: '', password: '', role: 'user'})
    await loadUsers()
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

async function toggleUser(user) {
  try {
    await adminApi.updateUser(user.id, {enabled: !user.enabled})
    await loadUsers()
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

async function resetPassword(user) {
  const password = window.prompt(`为 ${user.username} 设置新密码`)
  if (!password) return
  try {
    await adminApi.updateUser(user.id, {password})
    setNotice(`${user.username} 的密码已更新`)
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

async function createToken() {
  try {
    const created = await adminApi.createToken({name: tokenForm.name.trim(), scopes: tokenForm.scopes})
    issuedToken.value = created.token
    tokenFormOpen.value = false
    Object.assign(tokenForm, {name: '', scopes: []})
    await loadTokens()
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

function toggleScope(scope) {
  const index = tokenForm.scopes.indexOf(scope)
  if (index >= 0) {
    tokenForm.scopes.splice(index, 1)
  } else {
    tokenForm.scopes.push(scope)
  }
}

async function deleteToken(token) {
  if (!window.confirm(`确定吊销令牌“${token.name}”？使用它的系统会立即失去访问。`)) return
  try {
    await adminApi.deleteToken(token.id)
    setNotice('令牌已吊销')
    await loadTokens()
  } catch (error) {
    setNotice(friendlyError(error))
  }
}

onMounted(() => {
  loadUsers()
  loadTokens()
})
</script>

<template>
  <div class="admin">
    <p v-if="notice" class="notice">{{ notice }}</p>

    <section class="panel">
      <header class="row">
        <h3>用户</h3>
        <button class="primary" @click="userFormOpen = !userFormOpen">＋ 新增用户</button>
      </header>
      <form v-if="userFormOpen" class="inline-form" @submit.prevent="createUser">
        <input v-model="userForm.username" placeholder="用户名" required />
        <input v-model="userForm.display_name" placeholder="显示名（选填）" />
        <input v-model="userForm.password" type="password" placeholder="密码" required />
        <select v-model="userForm.role" aria-label="角色">
          <option value="user">普通用户</option>
          <option value="admin">管理员</option>
        </select>
        <button type="submit">创建</button>
      </form>
      <table>
        <thead>
          <tr><th>用户名</th><th>显示名</th><th>角色</th><th>状态</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="user in users" :key="user.id">
            <td>{{ user.username }}</td>
            <td>{{ user.display_name || '—' }}</td>
            <td>{{ user.role === 'admin' ? '管理员' : '普通用户' }}</td>
            <td>{{ user.enabled ? '启用' : '停用' }}</td>
            <td class="ops">
              <button class="link" @click="toggleUser(user)">{{ user.enabled ? '停用' : '启用' }}</button>
              <button class="link" @click="resetPassword(user)">重置密码</button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <section class="panel">
      <header class="row">
        <h3>对外接口令牌</h3>
        <button class="primary" @click="tokenFormOpen = !tokenFormOpen">＋ 签发令牌</button>
      </header>
      <p v-if="issuedToken" class="token-once">
        令牌只显示一次，请立即复制并交给接入系统：
        <code>{{ issuedToken }}</code>
        <button class="link" type="button" @click="issuedToken = ''">我已保存</button>
      </p>
      <form v-if="tokenFormOpen" class="inline-form" @submit.prevent="createToken">
        <input v-model="tokenForm.name" placeholder="令牌名称（如：系统A）" required />
        <label class="check"><input type="checkbox" :checked="tokenForm.scopes.includes('verify')" @change="toggleScope('verify')" /> 1:1 比对</label>
        <label class="check"><input type="checkbox" :checked="tokenForm.scopes.includes('identify')" @change="toggleScope('identify')" /> 1:N 识别</label>
        <label class="check"><input type="checkbox" :checked="tokenForm.scopes.includes('enroll')" @change="toggleScope('enroll')" /> 人脸录入</label>
        <button type="submit">签发</button>
      </form>
      <table>
        <thead>
          <tr><th>名称</th><th>权限</th><th>最近使用</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-if="!tokens.length">
            <td colspan="4" class="muted">还没有令牌。签发后把 token 与接口文档交给接入系统。</td>
          </tr>
          <tr v-for="token in tokens" :key="token.id">
            <td>{{ token.name }}</td>
            <td>{{ token.scopes }}</td>
            <td>{{ token.last_used_at || '从未使用' }}</td>
            <td class="ops">
              <button class="link danger" @click="deleteToken(token)">吊销</button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  </div>
</template>

<style scoped>
.admin {
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
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.inline-form {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
  padding: 12px;
  border: 1px dashed var(--line);
  border-radius: 8px;
  background: var(--bg);
}

.check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 14px;
  color: var(--muted);
}

table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

th, td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid var(--line);
}

th {
  color: var(--muted);
  font-weight: 600;
  font-size: 13px;
}

.ops {
  display: flex;
  gap: 8px;
}

button.link {
  border: none;
  background: none;
  padding: 0;
  font-size: 13px;
  color: var(--primary);
  cursor: pointer;
}

button.link.danger {
  color: var(--danger);
}

.token-once {
  background: rgba(245, 158, 11, 0.12);
  color: #92400e;
  border-radius: 8px;
  padding: 10px 12px;
  font-size: 14px;
  margin-bottom: 12px;
}

.token-once code {
  background: rgba(255, 255, 255, 0.6);
  border-radius: 4px;
  padding: 2px 6px;
  word-break: break-all;
  user-select: all;
}
</style>
