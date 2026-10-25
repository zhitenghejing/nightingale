import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Button, Space } from 'antd';
import { MinusOutlined, PlusOutlined } from '@ant-design/icons';

const MIN_SCALE = 0.02;
const MAX_SCALE = 2;
const PADDING = 24;
const CABINET_WIDTH = 590;

export default function RackViewport({ children }: { children: React.ReactNode }) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const cabinetRef = useRef<HTMLDivElement>(null);
  const [scale, setScale] = useState(1);
  const scaleRef = useRef(1);
  const [height, setHeight] = useState(0);
  const anchorRef = useRef<{ x: number; y: number; localX: number; localY: number }>();

  const autoFitRef = useRef(true);

  const zoomTo = useCallback((requested: number, pointer?: { x: number; y: number }) => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const current = scaleRef.current;
    const next = Math.min(MAX_SCALE, Math.max(MIN_SCALE, requested));
    if (Math.abs(next - current) < 0.0001) return;
    const x = pointer?.x ?? viewport.clientWidth / 2;
    const y = pointer?.y ?? viewport.clientHeight / 2;
    const originX = Math.max(PADDING, (viewport.clientWidth - CABINET_WIDTH * current) / 2);
    anchorRef.current = { x, y, localX: (viewport.scrollLeft + x - originX) / current, localY: (viewport.scrollTop + y - PADDING) / current };
    scaleRef.current = next;
    setScale(next);
  }, []);

  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    const anchor = anchorRef.current;
    if (!viewport || !anchor) return;
    const originX = Math.max(PADDING, (viewport.clientWidth - CABINET_WIDTH * scale) / 2);
    viewport.scrollLeft = anchor.localX * scale + originX - anchor.x;
    viewport.scrollTop = anchor.localY * scale + PADDING - anchor.y;
    anchorRef.current = undefined;
  }, [scale]);

  const manualZoom = useCallback(
    (requested: number, pointer?: { x: number; y: number }) => {
      autoFitRef.current = false;
      zoomTo(requested, pointer);
    },
    [zoomTo],
  );

  const fit = useCallback(() => {
    const viewport = viewportRef.current;
    const cabinet = cabinetRef.current;
    autoFitRef.current = true;
    if (!viewport || !cabinet?.offsetHeight || viewport.clientWidth <= PADDING * 2 || viewport.clientHeight <= PADDING * 2) return;
    zoomTo(Math.min((viewport.clientWidth - PADDING * 2) / CABINET_WIDTH, (viewport.clientHeight - PADDING * 2) / cabinet.offsetHeight, 1));
    // Fitting shows the whole cabinet; anchoring is only useful for manual zoom.
    anchorRef.current = undefined;
    viewport.scrollTop = 0;
    viewport.scrollLeft = 0;
  }, [zoomTo]);

  useLayoutEffect(() => {
    const cabinet = cabinetRef.current;
    const viewport = viewportRef.current;
    if (!cabinet || !viewport) return;
    const measure = () => {
      setHeight(cabinet.offsetHeight);
      if (autoFitRef.current) fit();
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(cabinet);
    observer.observe(viewport);
    return () => observer.disconnect();
  }, [fit]);

  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY === 0) return;
      event.preventDefault();
      const bounds = viewport.getBoundingClientRect();
      // Normalize mouse wheels and trackpads, limiting single-event jumps.
      const delta = event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewport.clientHeight : 1);
      manualZoom(scaleRef.current * Math.exp(-Math.max(-100, Math.min(100, delta)) * 0.002), { x: event.clientX - bounds.left, y: event.clientY - bounds.top });
    };
    // React delegates wheel events passively; a native listener is needed to stop page scrolling.
    viewport.addEventListener('wheel', onWheel, { passive: false });
    return () => viewport.removeEventListener('wheel', onWheel);
  }, [manualZoom]);

  return (
    <section className='rack-zoom'>
      <div className='rack-zoom-toolbar'>
        <Space size={8} wrap>
          <Button size='small' aria-label='缩小机柜图' icon={<MinusOutlined />} disabled={scale <= MIN_SCALE} onClick={() => manualZoom(scaleRef.current / 1.2)} />
          <output className='rack-zoom-value' aria-label='机柜图缩放比例' aria-live='polite'>
            {Math.round(scale * 100)}%
          </output>
          <Button size='small' aria-label='放大机柜图' icon={<PlusOutlined />} disabled={scale >= MAX_SCALE} onClick={() => manualZoom(scaleRef.current * 1.2)} />
          <Button size='small' onClick={fit}>
            适应窗口
          </Button>
        </Space>
      </div>
      <div className='rack-viewport' ref={viewportRef} role='region' aria-label='机柜图缩放区域' tabIndex={0}>
        <div className='rack-zoom-canvas' style={{ width: CABINET_WIDTH * scale + PADDING * 2, height: height * scale + PADDING * 2 }}>
          <div className='rack-zoom-object' ref={cabinetRef} style={{ width: CABINET_WIDTH, transform: `translateX(-50%) scale(${scale})` }}>
            {children}
          </div>
        </div>
      </div>
    </section>
  );
}
