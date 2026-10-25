import React, { useState } from 'react';
import type { Device } from './model';
import { matchMaterial, PanelSide } from './materialCatalog';
import { materialImages } from './materialImages';

interface Props {
  device: Device;
  side: PanelSide;
}
export default function AssetPanel({ device, side }: Props) {
  const matched = matchMaterial(device);
  const material = matched.status === 'matched' ? matched.material : undefined;
  const src = material ? materialImages[material.id]?.[side] : undefined;
  const [failed, setFailed] = useState('');
  if (src && failed !== src)
    return (
      <img
        className='rack-material-panel'
        src={src}
        alt={`${device.vendor} ${device.model} ${side === 'front' ? '正面' : '背面'}产品素材`}
        draggable={false}
        onError={() => setFailed(src)}
        data-material-id={material?.id}
        data-view={side}
      />
    );
  return (
    <svg
      className='rack-material-panel'
      viewBox='0 0 1200 120'
      preserveAspectRatio='none'
      role='img'
      aria-label={`${device.model || '未知型号'} ${side === 'front' ? '正面' : '背面'}素材未匹配`}
      data-view={side}
      data-material-id=''
    >
      <rect width='1200' height='120' fill='#343c46' />
      <rect x='8' y='8' width='1184' height='104' fill='#222a33' stroke='#737d89' />
      <text x='600' y='69' fill='#ced2db' fontSize='30' textAnchor='middle'>
        {src && failed === src ? '素材加载失败' : `${side === 'front' ? '正面' : '背面'}素材未匹配`}
      </text>
    </svg>
  );
}
