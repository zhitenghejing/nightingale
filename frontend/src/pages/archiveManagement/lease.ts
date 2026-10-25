import { basePrefix } from '@/App';
import { AccessTokenKey } from '@/utils/constant';
import { Editing, Lease } from './types';
import { acquire, api } from './services';
const key = (id: number) => `n9e.archive.editing.${id}`;
export function storedLease(id: number): Lease | undefined {
  try {
    return JSON.parse(sessionStorage.getItem(key(id)) || 'null') || undefined;
  } catch {
    return undefined;
  }
}
export function rememberLease(id: number, lease: Lease) {
  sessionStorage.setItem(key(id), JSON.stringify(lease));
}
export function forgetLease(id: number) {
  sessionStorage.removeItem(key(id));
}
export function bindWindow(id: number, current: () => Lease | undefined) {
  const channel = typeof BroadcastChannel === 'undefined' ? undefined : new BroadcastChannel('n9e.archive.editing');
  channel?.addEventListener('message', (e) => {
    const l = current();
    if (e.data?.type === 'probe' && e.data.documentID === id && l && l.session_id === e.data.sessionID && l.expires_at > Date.now())
      channel.postMessage({ type: 'active', probe: e.data.probe });
  });
  const hide = () => {
    const l = current();
    if (l)
      fetch(`${basePrefix}/api/n9e/archives/documents/${id}/lease`, {
        method: 'DELETE',
        keepalive: true,
        headers: { Authorization: `Bearer ${localStorage.getItem(AccessTokenKey) || ''}`, 'Content-Type': 'application/json' },
        body: JSON.stringify(l),
      }).catch(() => undefined);
  };
  window.addEventListener('pagehide', hide);
  return () => {
    channel?.close();
    window.removeEventListener('pagehide', hide);
  };
}
async function activeElsewhere(id: number, lease: Lease) {
  if (typeof BroadcastChannel === 'undefined') return false;
  const channel = new BroadcastChannel('n9e.archive.editing');
  const probe = crypto.randomUUID();
  let active = false;
  channel.addEventListener('message', (e) => {
    if (e.data?.type === 'active' && e.data.probe === probe) active = true;
  });
  channel.postMessage({ type: 'probe', probe, documentID: id, sessionID: lease.session_id });
  await new Promise((resolve) => setTimeout(resolve, 250));
  channel.close();
  return active;
}
export async function claim(id: number, resume?: Lease): Promise<Editing> {
  let result: Editing;
  if (resume) {
    if (await activeElsewhere(id, resume)) throw new Error('文档正在本账号的另一个窗口编辑，请回到原窗口操作');
    try {
      result = await api<Editing>(`/documents/${id}/lease/resume`, 'POST', resume);
    } catch {
      result = await acquire(id);
    }
  } else result = await acquire(id);
  rememberLease(id, result.lease);
  return result;
}
