import type { ApiEnvelope } from './types'

export class ApiError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export class AdminAuthError extends ApiError {
  constructor(message: string) {
    super(message, 401)
    this.name = 'AdminAuthError'
  }
}

export async function apiRequest<T>(
  input: string,
  init: RequestInit = {},
): Promise<T> {
  let response: Response
  try {
    response = await fetch(input, {
      credentials: 'same-origin',
      ...init,
      headers: {
        Accept: 'application/json',
        ...init.headers,
      },
    })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw error
    }
    throw new ApiError('无法连接服务器，请检查网络后重试。', 0)
  }

  const contentType = response.headers.get('Content-Type') ?? ''
  if (!contentType.includes('application/json')) {
    throw new ApiError(
      response.ok ? '服务器返回了无法识别的内容。' : `请求失败（${response.status}）。`,
      response.status,
    )
  }

  let payload: ApiEnvelope<T> | null = null
  try {
    payload = (await response.json()) as ApiEnvelope<T>
  } catch {
    throw new ApiError('服务器返回的内容无法解析。', response.status)
  }

  if (!response.ok || !payload.success) {
    const message = payload.error?.trim() || `请求失败（${response.status}）。`
    throw new ApiError(message, response.status)
  }

  return payload.data as T
}

export function jsonBody(value: unknown): RequestInit {
  return {
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(value),
  }
}
