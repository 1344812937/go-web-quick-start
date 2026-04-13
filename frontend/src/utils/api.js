export async function request(path, options = {}) {
  const response = await fetch(`/api${path}`, {
    headers: {
      'Content-Type': 'application/json',
      ...(options.headers || {}),
    },
    ...options,
  })

  const payload = await response.json()
  if (!payload.success) {
    throw new Error(payload.message || '请求失败')
  }
  return payload.data
}