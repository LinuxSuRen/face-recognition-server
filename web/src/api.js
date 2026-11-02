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

export const api = {request}
