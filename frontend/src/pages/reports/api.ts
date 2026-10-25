import { AccessTokenKey } from '@/utils/constant';

export type ReportKind = 'inspection' | 'incident';
export interface Report {
  id: string;
  kind: ReportKind;
  title: string;
  engineer: string;
  company: string;
  contact: string;
  reportDate: string;
  summary: string;
  filename: string;
  extension: string;
  size: number;
  sha256: string;
  ownerId: number;
  createdAt: string;
  updatedAt: string;
  version: number;
  previewStatus: 'pending' | 'ready' | 'failed';
  previewError?: string;
}
export interface ReportFields {
  title: string;
  engineer: string;
  company: string;
  contact: string;
  reportDate: string;
  summary: string;
}
export async function reportRequest(path = '', options: RequestInit = {}): Promise<Response> {
  const response = await fetch(`/api/cmdb/reports${path}`, {
    ...options,
    headers: { Authorization: `Bearer ${localStorage.getItem(AccessTokenKey) || ''}`, ...options.headers },
  });
  if (!response.ok) {
    const body = await response.json().catch(() => undefined);
    throw new Error(body?.err || `报告请求失败 (${response.status})`);
  }
  return response;
}
export async function listReports(kind: ReportKind, signal?: AbortSignal): Promise<Report[]> {
  const response = await reportRequest(`?kind=${kind}`, { signal });
  return (await response.json()).dat;
}
export async function saveReport(kind: ReportKind, fields: ReportFields, file?: File, existing?: Report): Promise<Report> {
  let response: Response;
  if (existing) {
    response = await reportRequest(`/${existing.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...fields, version: existing.version }),
    });
  } else {
    if (!file) throw new Error('请选择报告附件');
    const form = new FormData();
    form.append('metadata', JSON.stringify({ ...fields, kind }));
    form.append('file', file);
    response = await reportRequest('', { method: 'POST', body: form });
  }
  return (await response.json()).dat;
}
export async function reportBlob(report: Report, preview: boolean, signal?: AbortSignal): Promise<Blob> {
  const response = await reportRequest(`/${report.id}/${preview ? 'preview' : 'file'}`, { signal });
  const blob = await response.blob();
  return preview ? new Blob([blob], { type: 'application/pdf' }) : blob;
}
export function filterReports(rows: Report[], search: string): Report[] {
  const query = search.trim().toLocaleLowerCase();
  return rows.filter((r) => [r.title, r.engineer, r.company, r.summary, r.filename].some((v) => v.toLocaleLowerCase().includes(query)));
}
