import { apiRequest } from './http'
import type { SiteInfo } from './types'

export async function fetchSite(): Promise<SiteInfo> {
  const site = await apiRequest<Partial<SiteInfo>>('/api/site')
  return {
    name: site.name?.trim() || '文档',
    version: site.version?.trim() || '',
    count: typeof site.count === 'number' ? site.count : 0,
    adminEnabled: site.adminEnabled === true,
    downloadEnabled: site.downloadEnabled === true,
    showFileType: site.showFileType === true,
    showFileSize: site.showFileSize === true,
    theme: site.theme === 'forest' || site.theme === 'mono' ? site.theme : 'classic',
  }
}
