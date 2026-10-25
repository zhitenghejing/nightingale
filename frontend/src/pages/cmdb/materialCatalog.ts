import catalog from './materials/catalog.json';
import type { Device } from './model';

export type PanelSide = 'front' | 'rear';
interface MaterialSource {
  panel: string;
  original: string;
  imageUrl: string;
  sourcePage: string;
}
export interface Material {
  id: string;
  brand: string;
  vendor: string;
  vendorAliases: string[];
  model: string;
  modelAliases: string[];
  type: string;
  units: number;
  front: MaterialSource;
  rear: MaterialSource;
}
export interface MaterialMatch {
  status: 'matched' | 'unmatched' | 'conflict' | 'height-mismatch';
  material?: Material;
  message: string;
}
export const materials: Material[] = catalog;
export const normalize = (value: string) =>
  value
    .normalize('NFKC')
    .trim()
    .toLowerCase()
    .replace(/[\s_\-()./]+/g, '');
function normalizedModel(value: string): string {
  return normalize(value)
    .replace(/^(hewlettpackardenterprise|dellemc|浪潮信息|新华三|huawei|lenovo|inspur|戴尔|联想|华为|浪潮|惠普|华三|hpe|h3c|dell|hp)/, '')
    .replace(/^(proliant|poweredge|thinksystem|fusionserverpro|fusionserver)/, '');
}
function identityMatches(material: Material, device: Device): boolean {
  const vendor = normalize(device.vendor || '');
  const model = normalizedModel(device.model || '');
  return (
    material.vendorAliases.some((x) => normalize(x) === vendor) &&
    [material.model, ...material.modelAliases].some((x) => normalizedModel(x) === model) &&
    material.type === device.type
  );
}
export function matchMaterial(device: Device): MaterialMatch {
  let material: Material | undefined;
  if (device.assetMaterialId) {
    material = materials.find((x) => x.id === device.assetMaterialId);
    if (!material || !identityMatches(material, device)) return { status: 'conflict', message: '已选素材与厂商、完整型号或设备类型不一致，请重新选择素材型号' };
  } else {
    material = materials.find((x) => identityMatches(x, device));
  }
  if (!material) return { status: 'unmatched', message: '尚无该完整型号的素材，将显示通用占位面板' };
  if (device.uStart != null && device.uEnd != null && device.uEnd - device.uStart + 1 !== material.units)
    return { status: 'height-mismatch', material, message: `${material.model} 标准高度为 ${material.units}U，当前起止 U 位不符，请核实位置` };
  return { status: 'matched', material, message: `已匹配 ${material.brand} ${material.model} · ${material.units}U · 正背面产品素材` };
}
export function attachMaterial(device: Device): Device {
  const matched = matchMaterial(device);
  if (matched.status === 'conflict' || matched.status === 'height-mismatch') throw new Error(matched.message);
  return { ...device, assetMaterialId: matched.material?.id };
}

export function searchMaterials(query: string, material: Material): boolean {
  const keywords = normalize([material.brand, material.vendor, ...material.vendorAliases, material.model, ...material.modelAliases].join(' '));
  return query
    .trim()
    .split(/\s+/)
    .every((token) => keywords.includes(normalize(token)));
}
