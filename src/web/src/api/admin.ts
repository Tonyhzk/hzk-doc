import { AdminAuthError, ApiError, apiRequest, jsonBody } from './http'
import type {
  AdminDocumentResult,
  AdminEntries,
  AdminOrderResult,
  AdminRenameResult,
  AdminSession,
  AdminSource,
  AdminUploadResult,
  ApiEnvelope,
  PreviewResult,
  SiteTheme,
  TrashActionResult,
  TrashData,
  TrashEntry,
} from './types'

let csrfToken = ''

export function setAdminToken(token: string | undefined): void {
  csrfToken = token ?? ''
}

async function adminRequest<T>(
  input: string,
  init: RequestInit = {},
  write = false,
): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (write) {
    headers.set('X-CSRF-Token', csrfToken)
  }

  let response: Response
  try {
    response = await fetch(input, {
      credentials: 'same-origin',
      ...init,
      headers,
    })
  } catch {
    throw new ApiError('无法连接服务器，请检查网络后重试。', 0)
  }

  let payload: ApiEnvelope<T> | null = null
  const contentType = response.headers.get('Content-Type') ?? ''
  if (contentType.includes('application/json')) {
    try {
      payload = (await response.json()) as ApiEnvelope<T>
    } catch {
      payload = null
    }
  }

  if (response.status === 401) {
    throw new AdminAuthError(payload?.error ?? '登录已过期，请重新登录。')
  }
  if (!response.ok || !payload?.success) {
    throw new ApiError(
      payload?.error?.trim() || `请求失败（${response.status}）。`,
      response.status,
    )
  }
  return payload.data as T
}

export async function fetchSession(): Promise<AdminSession> {
  const session = await apiRequest<AdminSession>('/api/admin/session')
  setAdminToken(session.csrfToken)
  return session
}

export async function login(password: string): Promise<AdminSession> {
  const session = await adminRequest<AdminSession>('/api/admin/login', {
    method: 'POST',
    ...jsonBody({ password }),
  })
  setAdminToken(session.csrfToken)
  return session
}

export async function logout(): Promise<AdminSession> {
  const session = await adminRequest<AdminSession>(
    '/api/admin/logout',
    { method: 'POST' },
    true,
  )
  setAdminToken(undefined)
  return session
}

export async function saveSiteName(name: string): Promise<{ name: string }> {
  return adminRequest<{ name: string }>(
    '/api/admin/site-name',
    { method: 'PUT', ...jsonBody({ name }) },
    true,
  )
}

export async function saveSiteTheme(theme: SiteTheme): Promise<{ theme: SiteTheme }> {
  return adminRequest<{ theme: SiteTheme }>(
    '/api/admin/theme',
    { method: 'PUT', ...jsonBody({ theme }) },
    true,
  )
}

export async function fetchEntries(directory: string): Promise<AdminEntries> {
  const query = directory ? `?dir=${encodeURIComponent(directory)}` : ''
  return adminRequest<AdminEntries>(`/api/admin/entries${query}`)
}

export async function fetchSource(path: string): Promise<AdminSource> {
  return adminRequest<AdminSource>(
    `/api/admin/source?path=${encodeURIComponent(path)}`,
  )
}

export async function createDocument(
  path: string,
  content: string,
): Promise<AdminDocumentResult> {
  return adminRequest<AdminDocumentResult>(
    '/api/admin/document',
    {
      method: 'POST',
      ...jsonBody({ path, content }),
    },
    true,
  )
}

export async function saveDocument(
  path: string,
  content: string,
  revision: string,
): Promise<AdminDocumentResult> {
  return adminRequest<AdminDocumentResult>(
    '/api/admin/document',
    {
      method: 'PUT',
      ...jsonBody({ path, content, revision }),
    },
    true,
  )
}

export async function createFolder(path: string): Promise<{ path: string }> {
  return adminRequest<{ path: string }>(
    '/api/admin/folder',
    {
      method: 'POST',
      ...jsonBody({ path }),
    },
    true,
  )
}

export async function renameDocument(
  path: string,
  name: string,
): Promise<AdminRenameResult> {
  return adminRequest<AdminRenameResult>(
    '/api/admin/rename',
    {
      method: 'POST',
      ...jsonBody({ path, name }),
    },
    true,
  )
}

export async function saveOrder(
  directory: string,
  names: string[],
): Promise<AdminOrderResult> {
  return adminRequest<AdminOrderResult>(
    '/api/admin/order',
    {
      method: 'PUT',
      ...jsonBody({ directory, names }),
    },
    true,
  )
}

export async function previewSource(
  path: string,
  content: string,
): Promise<PreviewResult> {
  return adminRequest<PreviewResult>(
    '/api/admin/preview',
    {
      method: 'POST',
      ...jsonBody({ path, content }),
    },
    true,
  )
}

export async function deleteEntry(path: string): Promise<TrashEntry> {
  return adminRequest<TrashEntry>(
    '/api/admin/delete',
    {
      method: 'POST',
      ...jsonBody({ path }),
    },
    true,
  )
}

export async function fetchTrash(): Promise<TrashData> {
  return adminRequest<TrashData>('/api/admin/trash')
}

export async function restoreTrash(ids: string[]): Promise<TrashActionResult> {
  return adminRequest<TrashActionResult>(
    '/api/admin/trash/restore',
    {
      method: 'POST',
      ...jsonBody({ ids }),
    },
    true,
  )
}

export async function purgeTrash(ids: string[]): Promise<TrashActionResult> {
  return adminRequest<TrashActionResult>(
    '/api/admin/trash/purge',
    {
      method: 'POST',
      ...jsonBody({ ids }),
    },
    true,
  )
}

export async function emptyTrash(): Promise<TrashActionResult> {
  return adminRequest<TrashActionResult>(
    '/api/admin/trash/empty',
    {
      method: 'POST',
      ...jsonBody({}),
    },
    true,
  )
}

export function uploadFile(
  file: File,
  directory: string,
  onProgress: (percent: number) => void,
  signal?: AbortSignal,
): Promise<AdminUploadResult> {
  return new Promise((resolve, reject) => {
    const form = new FormData()
    form.append('file', file)
    form.append('directory', directory)

    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/admin/upload')
    xhr.withCredentials = true
    xhr.setRequestHeader('Accept', 'application/json')
    xhr.setRequestHeader('X-CSRF-Token', csrfToken)
    xhr.upload.addEventListener('progress', (event) => {
      if (event.lengthComputable) {
        onProgress(Math.round((event.loaded / event.total) * 100))
      }
    })
    xhr.addEventListener('load', () => {
      let payload: ApiEnvelope<AdminUploadResult> | null = null
      try {
        payload = JSON.parse(xhr.responseText) as ApiEnvelope<AdminUploadResult>
      } catch {
        payload = null
      }
      if (xhr.status === 401) {
        reject(new AdminAuthError(payload?.error ?? '登录已过期，请重新登录。'))
        return
      }
      if (xhr.status < 200 || xhr.status >= 300 || !payload?.success) {
        reject(
          new ApiError(
            payload?.error?.trim() || `上传失败（${xhr.status}）。`,
            xhr.status,
          ),
        )
        return
      }
      onProgress(100)
      resolve(payload.data as AdminUploadResult)
    })
    xhr.addEventListener('error', () => {
      reject(new ApiError('上传中断，请检查网络后重试。', 0))
    })
    xhr.addEventListener('abort', () => {
      reject(new DOMException('上传已取消', 'AbortError'))
    })
    signal?.addEventListener('abort', () => xhr.abort(), { once: true })
    xhr.send(form)
  })
}
