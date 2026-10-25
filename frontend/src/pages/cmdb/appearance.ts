import { Device } from './model';
import { DeviceBrand, DeviceKind } from '@/pages/rackViews/catalog';
// A template is selected only when both model family and physical height agree.
// Unsupported models retain their actual label and use the generic faceplate.
export function appearance(device: Device): { kind: DeviceKind; brand: DeviceBrand } {
  const model = device.model.toLowerCase().replace(/[\s_-]/g, '');
  const vendor = device.vendor.toLowerCase();
  const height = (device.uEnd || 1) - (device.uStart || 1) + 1;
  if (device.type === '机架式服务器') {
    if (vendor.includes('dell') && ((model.includes('r650') && height === 1) || (model.includes('r750') && height === 2))) return { kind: 'server', brand: 'dell' };
    if ((vendor.includes('hpe') || vendor.includes('hp')) && ((model.includes('dl360') && height === 1) || (model.includes('dl380') && height === 2)))
      return { kind: 'server', brand: 'hpe' };
    if ((vendor.includes('huawei') || vendor.includes('华为')) && model.includes('2288') && height === 2) return { kind: 'server', brand: 'huawei' };
  }
  if (device.type === '交换机' && height === 1) {
    if ((vendor.includes('huawei') || vendor.includes('华为')) && /^s57/.test(model)) return { kind: 'switch', brand: 'huawei' };
    if (vendor.includes('h3c') && /^s51/.test(model)) return { kind: 'switch', brand: 'h3c' };
    if ((vendor.includes('ruijie') || vendor.includes('锐捷')) && /^rgs/.test(model)) return { kind: 'switch', brand: 'ruijie' };
  }
  return { kind: 'generic', brand: 'generic' };
}
