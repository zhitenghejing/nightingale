import React, { useEffect, useRef } from 'react';

export const MIN_COLUMN_WIDTH = 90;
export const MAX_COLUMN_WIDTH = 1200;
export const clampColumnWidth = (width: number) => Math.max(MIN_COLUMN_WIDTH, Math.min(MAX_COLUMN_WIDTH, width));

interface Props extends React.ThHTMLAttributes<HTMLTableCellElement> {
  width?: number;
  resizeLabel?: string;
  onColumnResize?: (width: number) => void;
}

export default function ResizableHeaderCell({ width, resizeLabel, onColumnResize, children, ...rest }: Props) {
  const stop = useRef<() => void>();
  useEffect(() => () => stop.current?.(), []);
  return (
    <th {...rest} style={{ ...rest.style, ...(width ? { width } : {}) }}>
      {children}
      {width && onColumnResize && (
        <span
          className='device-inventory-resize'
          role='separator'
          aria-label={resizeLabel}
          aria-orientation='vertical'
          aria-valuemin={MIN_COLUMN_WIDTH}
          aria-valuemax={MAX_COLUMN_WIDTH}
          aria-valuenow={width}
          tabIndex={0}
          onClick={(e) => e.stopPropagation()}
          onKeyDown={(e) => {
            if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
              e.preventDefault();
              e.stopPropagation();
              onColumnResize(clampColumnWidth(width + (e.key === 'ArrowRight' ? 16 : -16)));
            }
          }}
          onPointerDown={(e) => {
            if (e.button !== 0) return;
            e.preventDefault();
            e.stopPropagation();
            stop.current?.();
            const x = e.clientX;
            const move = (event: PointerEvent) => onColumnResize(clampColumnWidth(width + event.clientX - x));
            const finish = () => {
              document.removeEventListener('pointermove', move);
              document.removeEventListener('pointerup', finish);
              document.removeEventListener('pointercancel', finish);
              window.removeEventListener('blur', finish);
              document.body.classList.remove('device-inventory-resizing');
              stop.current = undefined;
            };
            stop.current = finish;
            document.body.classList.add('device-inventory-resizing');
            document.addEventListener('pointermove', move);
            document.addEventListener('pointerup', finish);
            document.addEventListener('pointercancel', finish);
            window.addEventListener('blur', finish);
          }}
        />
      )}
    </th>
  );
}
