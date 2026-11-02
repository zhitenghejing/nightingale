import React, { useEffect, useState } from 'react';
import { AppstoreOutlined } from '@ant-design/icons';

export default function TemplateLogo({ src, small = false }: { src?: string; small?: boolean }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [src]);
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: small ? 28 : 68,
        height: small ? 28 : 42,
        fontSize: small ? 24 : 36,
        color: '#788199',
      }}
    >
      {src && !failed ? <img src={src} alt='' onError={() => setFailed(true)} style={{ height: '100%', maxWidth: '100%', objectFit: 'contain' }} /> : <AppstoreOutlined />}
    </span>
  );
}
