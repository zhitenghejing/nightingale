import type { PageViewport } from 'pdfjs-dist/types/src/display/page_viewport';
export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}
export type Viewport = Pick<PageViewport, 'viewBox' | 'convertToViewportPoint' | 'convertToPdfPoint'>;
// Coordinates refer to the visible unrotated PDF box, including its origin.
// PDF.js 6 exposes point conversions; rectangle conversion was removed.
export function annotationToViewport(r: Rect, viewport: Viewport) {
  const [left, bottom, right, top] = viewport.viewBox;
  const width = right - left;
  const height = top - bottom;
  const p1 = viewport.convertToViewportPoint(left + r.x * width, top - (r.y + r.height) * height);
  const p2 = viewport.convertToViewportPoint(left + (r.x + r.width) * width, top - r.y * height);
  return { left: Math.min(p1[0], p2[0]), top: Math.min(p1[1], p2[1]), width: Math.abs(p2[0] - p1[0]), height: Math.abs(p2[1] - p1[1]) };
}
export function viewportToAnnotation(r: { left: number; top: number; right: number; bottom: number }, viewport: Viewport): Rect {
  const [left, bottom, right, top] = viewport.viewBox;
  const width = right - left;
  const height = top - bottom;
  const p1 = viewport.convertToPdfPoint(r.left, r.top);
  const p2 = viewport.convertToPdfPoint(r.right, r.bottom);
  const x = Math.max(0, Math.min(1, (Math.min(p1[0], p2[0]) - left) / width));
  const y = Math.max(0, Math.min(1, (top - Math.max(p1[1], p2[1])) / height));
  return { x, y, width: Math.max(0, Math.min(1 - x, Math.abs(p2[0] - p1[0]) / width)), height: Math.max(0, Math.min(1 - y, Math.abs(p2[1] - p1[1]) / height)) };
}
