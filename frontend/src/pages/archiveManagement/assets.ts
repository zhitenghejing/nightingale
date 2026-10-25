import { Body, Lease } from './types';
import { fileBlob, upload } from './services';
export class DocumentAssets {
  private urls = new Map<string, string>();
  private reverse = new Map<string, string>();
  async display(body: Body): Promise<Body> {
    const result = JSON.parse(JSON.stringify(body));
    const walk = async (v: any) => {
      if (!v || typeof v !== 'object') return;
      if (v.type === 'image' && /^archive-asset:\d+$/.test(v.value)) {
        const key = v.value;
        if (!this.urls.has(key)) {
          const url = URL.createObjectURL(await fileBlob(Number(key.split(':')[1])));
          this.urls.set(key, url);
          this.reverse.set(url, key);
        }
        v.value = this.urls.get(key);
      }
      await Promise.all(
        Object.values(v)
          .filter((x) => x && typeof x === 'object')
          .map(walk),
      );
    };
    await walk(result.data);
    return result;
  }
  async canonical(body: Body, archiveID: number, documentID = 0, lease?: Lease): Promise<Body> {
    const result = JSON.parse(JSON.stringify(body));
    const walk = async (v: any) => {
      if (!v || typeof v !== 'object') return;
      if (v.type === 'image') {
        if (this.reverse.has(v.value)) v.value = this.reverse.get(v.value);
        else if (/^data:image\/(png|jpeg);base64,/.test(v.value)) {
          const response = await fetch(v.value);
          const blob = await response.blob();
          const asset = await upload(archiveID, new File([blob], blob.type === 'image/png' ? 'image.png' : 'image.jpg', { type: blob.type }), '', documentID, lease, true);
          const key = `archive-asset:${asset.id}`;
          this.reverse.set(v.value, key);
          v.value = key;
        } else if (!/^archive-asset:\d+$/.test(v.value)) throw new Error('文档包含未受管或不支持的图片，请将图片保存为 PNG/JPG 后重新插入');
      }
      for (const child of Object.values(v)) if (child && typeof child === 'object') await walk(child);
    };
    await walk(result.data);
    result.schema_version = 1;
    // Editing state and search/zoom do not define the document layout.
    if (result.options) {
      result.options.mode = 'edit';
      result.options.scale = 1;
      result.options.scrollContainerSelector = '';
    }
    return result;
  }
  async insertImage(file: File, archiveID: number, documentID: number, lease: Lease) {
    if (!['image/png', 'image/jpeg'].includes(file.type) || file.size > 10 * 1048576) throw new Error('图片仅支持 PNG/JPG，单张不超过 10 MB');
    const asset = await upload(archiveID, file, '', documentID, lease, true);
    const key = `archive-asset:${asset.id}`;
    const url = URL.createObjectURL(file);
    this.urls.set(key, url);
    this.reverse.set(url, key);
    const bitmap = await createImageBitmap(file);
    const ratio = Math.min(1, 650 / bitmap.width);
    const image = { value: url, width: bitmap.width * ratio, height: bitmap.height * ratio };
    bitmap.close();
    return image;
  }
  url(key: string) {
    return this.urls.get(key);
  }
  destroy() {
    this.urls.forEach((v) => URL.revokeObjectURL(v));
    this.urls.clear();
    this.reverse.clear();
  }
}
