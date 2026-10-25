export type DeviceKind = 'server' | 'switch' | 'generic';
export type DeviceBrand = 'hpe' | 'dell' | 'huawei' | 'h3c' | 'ruijie' | 'generic';
export const brands: Record<DeviceKind, { value: DeviceBrand; label: string }[]> = {
  server: [
    { value: 'hpe', label: '惠普 HPE' },
    { value: 'dell', label: '戴尔 Dell' },
    { value: 'huawei', label: '华为 Huawei' },
  ],
  switch: [
    { value: 'huawei', label: '华为 Huawei' },
    { value: 'h3c', label: '华三 H3C' },
    { value: 'ruijie', label: '锐捷 Ruijie' },
  ],
  generic: [{ value: 'generic', label: '通用' }],
};
export interface AppearanceInput {
  kind?: DeviceKind;
  brand?: DeviceBrand;
  size: number;
}
export function appearanceError(device: AppearanceInput): string | undefined {
  if (device.kind == null && device.brand == null) return;
  if (!device.kind || !brands[device.kind]?.some((brand) => brand.value === device.brand)) return '请选择与设备类型匹配的品牌';
  if (device.kind === 'server' && device.brand === 'huawei' && device.size !== 2) return '当前华为服务器素材为 2U';
  if (device.kind === 'server' && ![1, 2].includes(device.size)) return '当前服务器素材支持 1U 或 2U';
  if (device.kind === 'switch' && device.size !== 1) return '当前交换机素材为 1U';
}
export function getAppearance(device: AppearanceInput) {
  if (appearanceError(device)) return { variant: 'generic', label: '通用设备' };
  if (device.kind === 'server') {
    if (device.brand === 'huawei') return { variant: 'huawei-2u', label: 'Huawei 2U 服务器' };
    if (device.brand === 'dell' && device.size === 2) return { variant: 'dell-2u', label: 'Dell PowerEdge 2U' };
    const model = device.brand === 'hpe' ? (device.size === 1 ? 'DL360 Gen10' : 'DL380 Gen10') : device.size === 1 ? 'R650' : 'R750';
    return { variant: `${device.brand}-${device.size}u`, label: `${device.brand === 'hpe' ? 'HPE ProLiant' : 'Dell PowerEdge'} ${model}` };
  }
  if (device.kind === 'switch') return { variant: device.brand!, label: `${brands.switch.find((brand) => brand.value === device.brand)!.label} 交换机` };
  return { variant: 'generic', label: '通用设备' };
}
