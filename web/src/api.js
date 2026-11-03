// Thin fetch wrapper: same-origin requests, JSON in/out, and one error shape
// ({message}) for the UI to render. Technical error details are wrapped into
// friendly copy at the call sites instead of being shown verbatim.
async function request(path, options = {}) {
  const response = await fetch(path, {
    headers: options.body instanceof FormData ? undefined : {'Content-Type': 'application/json'},
    ...options,
  })
  if (response.status === 204) {
    return null
  }
  const text = await response.text()
  const data = text ? JSON.parse(text) : null
  if (!response.ok) {
    const detail = data && data.detail ? data.detail : `请求失败（HTTP ${response.status}）`
    throw new Error(detail)
  }
  return data
}

function queryString(params = {}) {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null) {
      search.set(key, value)
    }
  }
  const text = search.toString()
  return text ? `?${text}` : ''
}

export const api = {request}

export const facesApi = {
  listEmployees(params) {
    return request(`/api/v1/faces/employees${queryString(params)}`)
  },
  createEmployee(body) {
    return request('/api/v1/faces/employees', {method: 'POST', body: JSON.stringify(body)})
  },
  updateEmployee(id, body) {
    return request(`/api/v1/faces/employees/${id}`, {method: 'PATCH', body: JSON.stringify(body)})
  },
  deleteEmployee(id) {
    return request(`/api/v1/faces/employees/${id}`, {method: 'DELETE'})
  },
  listSamples(employeeId) {
    return request(`/api/v1/faces/employees/${employeeId}/samples`)
  },
  uploadSample(employeeId, file) {
    const form = new FormData()
    form.append('file', file)
    return request(`/api/v1/faces/employees/${employeeId}/samples`, {method: 'POST', body: form})
  },
  deleteSample(employeeId, sampleId) {
    return request(`/api/v1/faces/employees/${employeeId}/samples/${sampleId}`, {method: 'DELETE'})
  },
  recognize(file) {
    const form = new FormData()
    form.append('image', file)
    return request('/api/v1/faces/recognize', {method: 'POST', body: form})
  },
}
