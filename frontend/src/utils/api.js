export async function request(path, options = {}) {
  let response
  try {
    response = await fetch(`/api${path}`, {
      headers: {
        'Content-Type': 'application/json',
        ...(options.headers || {}),
      },
      ...options,
    })
  } catch {
    throw new Error('无法连接到后端服务，请确认 Go 服务已启动（go run .）')
  }

  const responseText = await response.text()
  if (!responseText.trim()) {
    if (!response.ok) {
      throw new Error('后端服务不可用，请确认 Go 服务已启动（go run .）')
    }
    throw new Error('后端服务未返回数据')
  }

  let payload
  try {
    payload = JSON.parse(responseText)
  } catch {
    throw new Error(`后端响应格式错误（HTTP ${response.status}）`)
  }

  if (!response.ok || !payload.success) {
    throw new Error(payload.message || `请求失败（HTTP ${response.status}）`)
  }
  return payload.data
}
