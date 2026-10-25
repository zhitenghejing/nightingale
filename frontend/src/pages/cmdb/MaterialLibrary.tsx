import React, { useState } from 'react';
import { Button, Input, Modal, Tag } from 'antd';
import { materials, searchMaterials } from './materialCatalog';
import { materialImages } from './materialImages';

export default function MaterialLibrary() {
  const [visible, setVisible] = useState(false);
  const [query, setQuery] = useState('');
  const close = () => {
    setVisible(false);
    setQuery('');
  };
  return (
    <>
      <Button onClick={() => setVisible(true)}>设备素材库</Button>
      <Modal
        title='设备矢量素材库 · 10 个型号 / 20 个正背面素材'
        visible={visible}
        width={1280}
        footer={<Button onClick={close}>关闭</Button>}
        onCancel={close}
        destroyOnClose
        bodyStyle={{ maxHeight: '75vh', overflowY: 'auto' }}
      >
        <Input.Search className='mb-4' value={query} onChange={(e) => setQuery(e.target.value)} allowClear placeholder='搜索品牌、完整型号或型号别名' />
        <div className='cmdb-material-library-grid'>
          {materials
            .filter((m) => searchMaterials(query, m))
            .map((m) => (
              <article key={m.id} className='cmdb-material-card'>
                <div className='flex items-center justify-between mb-3'>
                  <strong>
                    {m.brand} {m.model}
                  </strong>
                  <Tag>{m.units}U</Tag>
                </div>
                {(['front', 'rear'] as const).map((side) => (
                  <figure key={side}>
                    <figcaption>{side === 'front' ? '正面' : '背面'}</figcaption>
                    <img
                      src={materialImages[m.id][side]}
                      alt={`${m.model} ${side === 'front' ? '正面' : '背面'}`}
                      loading='lazy'
                      draggable={false}
                      style={{ aspectRatio: `10 / ${m.units}` }}
                    />
                    <a href={m[side].sourcePage} target='_blank' rel='noreferrer'>
                      参考来源
                    </a>
                  </figure>
                ))}
              </article>
            ))}
        </div>
      </Modal>
    </>
  );
}
