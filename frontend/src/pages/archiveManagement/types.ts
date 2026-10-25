export type Category = 'training' | 'inspection' | 'incident' | 'other';
export type DocKind = 'online' | 'docx' | 'pdf' | 'image';
export interface Metadata {
  title?: string;
  department?: string;
  location?: string;
  training_status?: string;
  start_time?: string;
  end_time?: string;
  duration?: number | null;
  lecturer?: string;
  participants?: string;
  training_content?: string;
  training_effect?: string;
  inspection_result?: string;
  scope?: string[];
  handler?: string;
  summary?: string;
  level?: string;
  affected_business?: string;
  occurred_at?: string;
  resolved_at?: string;
  main_engineer?: string;
  auxiliary_engineers?: string;
  custom_type?: string;
}
export interface Body {
  schema_version?: number;
  data: { main: any[]; header?: any[]; footer?: any[]; [key: string]: any };
  options?: any;
}
export interface BlobInfo {
  id: number;
  name: string;
  content_type: string;
  size: number;
  kind: DocKind;
  sha256: string;
}
export interface DocVersion {
  id: number;
  document_id: number;
  sequence: number;
  blob_id: number;
  body: Body | AnnotationBody | null;
  size: number;
  description: string;
  create_at: number;
  create_name: string;
}
export interface ArchiveDocument {
  id: number;
  archive_id: number;
  title: string;
  purpose: string;
  kind: DocKind;
  source_document_id: number;
  source_version_id: number;
  current_version_id: number;
  version: DocVersion;
  file?: BlobInfo;
  deleted: boolean;
}
export interface Archive {
  id: number;
  category: Category;
  title: string;
  state: string;
  metadata: Metadata;
  revision: number;
  deleted: boolean;
  primary_document_id: number;
  create_at: number;
  create_name: string;
  update_at: number;
  update_name: string;
  documents?: ArchiveDocument[];
  current_bytes?: number;
}
export interface Lease {
  session_id: string;
  epoch: number;
  expires_at: number;
  base_version_id: number;
  owner_name: string;
}
export interface Draft {
  body: Body | AnnotationBody;
  mode: string;
  markdown: string;
  base_version_id: number;
  update_at: number;
}
export interface Editing {
  lease: Lease;
  document: ArchiveDocument;
  draft?: Draft;
}
export interface Annotation {
  id: string;
  type: 'highlight' | 'strikeout' | 'note';
  page: number;
  rects: { x: number; y: number; width: number; height: number }[];
  text: string;
  color: string;
  author_name?: string;
  create_at?: number;
  update_at?: number;
}
export interface AnnotationBody {
  annotations: Annotation[];
}
export interface Version {
  id: number;
  sequence: number;
  action?: string;
  description?: string;
  create_at: number;
  create_name: string;
  snapshot?: { record: Archive; documents: { document: ArchiveDocument; version_id: number }[] };
}
export interface Log {
  id: number;
  action: string;
  document_id: number;
  detail: any;
  actor_name: string;
  actor_username: string;
  create_at: number;
}
export interface ExportJob {
  id: string;
  state: string;
  error: string;
  file_name: string;
  format: string;
}
export const categories: { key: Category; label: string }[] = [
  { key: 'training', label: '培训服务' },
  { key: 'inspection', label: '巡检报告' },
  { key: 'incident', label: '故障报告' },
  { key: 'other', label: '其他文档' },
];
export const purposes: Record<Category, { value: string; label: string }[]> = {
  training: [
    { value: 'training_material', label: '培训讲义' },
    { value: 'attendance', label: '参会人员签到' },
    { value: 'other', label: '其他附件' },
  ],
  inspection: [
    { value: 'inspection_report', label: '巡检报告' },
    { value: 'other', label: '其他附件' },
  ],
  incident: [
    { value: 'incident_report', label: '故障报告' },
    { value: 'other', label: '其他附件' },
  ],
  other: [{ value: 'other', label: '文档资料' }],
};
export const states = [
  { value: 'draft', label: '草稿' },
  { value: 'archived', label: '已归档' },
];
export const emptyBody = (): Body => ({
  schema_version: 1,
  data: { main: [{ value: '' }], header: [], footer: [] },
  options: { defaultFont: 'Noto Sans CJK SC', defaultSize: 16, width: 794, height: 1123, margins: [72, 72, 72, 72], pageNumber: { disabled: false } },
});
export const fmtDate = (n?: number | string) => (n ? new Date(n).toLocaleString('zh-CN', { hour12: false }) : '—');
export const fmtBytes = (n: number = 0) => (n >= 1048576 ? `${(n / 1048576).toFixed(1)} MB` : `${Math.ceil(n / 1024)} KB`);
