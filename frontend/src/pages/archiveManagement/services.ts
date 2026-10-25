import request from '@/utils/request';
import { Archive, ArchiveDocument, BlobInfo, Editing, ExportJob, Lease, Metadata, Version, Log } from './types';
const base = '/api/n9e/archives';
export async function api<T = any>(path = '', method = 'GET', data?: any): Promise<T> {
  const res = await request(base + path, { method, data, silence: true });
  if (!res || res.err !== '') throw new Error(res?.err || '请求失败');
  return res.dat;
}
export const listArchives = (params: Record<string, any>) =>
  api<{ list: Archive[]; total: number }>(
    '?' +
      new URLSearchParams(
        Object.entries(params)
          .filter(([, v]) => v !== '' && v !== undefined)
          .map(([k, v]) => [k, String(v)]),
      ).toString(),
  );
export const getArchive = (id: number) => api<Archive>(`/${id}`);
export const createArchive = (category: string, metadata: Metadata) => api<Archive>('', 'POST', { category, metadata });
export const getDocument = (id: number, version?: number) => api<ArchiveDocument>(`/documents/${id}${version ? `?version_id=${version}` : ''}`);
export const acquire = (id: number) => api<Editing>(`/documents/${id}/lease`, 'POST', {});
export const release = (id: number, lease: Lease) => api(`/documents/${id}/lease`, 'DELETE', lease);
export async function upload(archiveID: number, file: File, purpose: string, documentID = 0, lease?: Lease, asset = false) {
  const data = new FormData();
  data.append('file', file);
  data.append('purpose', purpose);
  data.append('document_id', String(documentID));
  if (lease) {
    data.append('session_id', lease.session_id);
    data.append('epoch', String(lease.epoch));
  }
  const res = await request(`${base}/${archiveID}/${asset ? 'assets' : 'files'}`, { method: 'POST', data, requestType: 'form', silence: true });
  if (!res || res.err !== '') throw new Error(res?.err || '上传失败');
  return res.dat as BlobInfo & Archive;
}
export const versions = (id: number, document = false, offset = 0) =>
  api<{ list: Version[]; total: number }>(document ? `/documents/${id}/versions?offset=${offset}` : `/${id}/versions?offset=${offset}`);
export const logs = (id: number, offset = 0) => api<Log[]>(`/${id}/logs?offset=${offset}`);
export async function binary(path: string): Promise<Blob> {
  return request(base + path, { responseType: 'blob', silence: true });
}
export const fileBlob = (id: number, download = false) => binary(`/files/${id}${download ? '?download=true' : ''}`);
export async function downloadBlob(blob: Blob, name: string) {
  const u = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = u;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(u), 1000);
}
export async function exportDocument(
  archiveID: number,
  format: string,
  documentID?: number,
  versionID?: number,
  signal?: AbortSignal,
  forReading = false,
): Promise<{ job: ExportJob; blob: Blob }> {
  let job = await api<ExportJob>(`/${archiveID}/exports`, 'POST', { format, document_id: documentID || 0, version_id: versionID || 0, for_reading: forReading });
  const until = Date.now() + 240000;
  while (!['done', 'error'].includes(job.state)) {
    if (signal?.aborted) throw new Error('已取消等待，导出任务仍会保留');
    if (Date.now() > until) throw new Error('导出仍在处理中，请稍后重试');
    await new Promise((r) => setTimeout(r, 1000));
    job = await api<ExportJob>(`/exports/${job.id}`);
  }
  if (job.state === 'error') throw new Error(job.error);
  return { job, blob: await binary(`/exports/${job.id}/file${forReading ? '?inline=true' : ''}`) };
}
export const errorText = (e: any) => {
  const text = e?.data?.error?.message || e?.data?.err || e?.message || '操作失败，请重试';
  const owner = e?.data?.error?.current?.owner;
  return owner ? `${text}（${owner}）` : text;
};
