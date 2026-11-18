// Throwaway switcher shared by the three alert-operations UI variants.
import React, { useEffect } from 'react';
import { useHistory, useLocation } from 'react-router-dom';
import { LeftOutlined, RightOutlined } from '@ant-design/icons';

export const prototypeHost = () => /^(localhost|127\.0\.0\.1|192\.168\.[\d.]+|n9e-dev[0-9]\.zhiteng\.win)$/.test(window.location.hostname);
export default function PrototypeSwitcher({ current, variants }: { current: string; variants: { key: string; name: string }[] }) {
  const history = useHistory();
  const location = useLocation();
  const cycle = (direction: number) => {
    const index = variants.findIndex((v) => v.key === current);
    const next = variants[(index + direction + variants.length) % variants.length];
    const query = new URLSearchParams(location.search);
    query.set('variant', next.key);
    history.replace({ pathname: location.pathname, search: query.toString() });
  };
  useEffect(() => {
    const keydown = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (el.closest('input,textarea,select,[contenteditable="true"],[role="dialog"],[role="combobox"]') || e.altKey || e.ctrlKey || e.metaKey) return;
      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        e.preventDefault();
        cycle(e.key === 'ArrowLeft' ? -1 : 1);
      }
    };
    window.addEventListener('keydown', keydown);
    return () => window.removeEventListener('keydown', keydown);
  }, [current, location.search]);
  // A production build on a development preview is allowed; stable hosts never show prototypes.
  if (!prototypeHost()) return null;
  return (
    <nav className='ops-prototype-switcher' aria-label='切换原型布局'>
      <button aria-label='上一个布局' onClick={() => cycle(-1)}>
        <LeftOutlined />
      </button>
      <span>
        <small>
          页面方案 {current} / {variants.length}
        </small>
        <strong>{variants.find((v) => v.key === current)?.name}</strong>
      </span>
      <button aria-label='下一个布局' onClick={() => cycle(1)}>
        <RightOutlined />
      </button>
    </nav>
  );
}
