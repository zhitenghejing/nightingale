import { annotationToViewport, viewportToAnnotation, Viewport } from './pdfGeometry';
const crop = [10, 20, 210, 120];
const rect = { x: 0.25, y: 0.2, width: 0.5, height: 0.3 };
test('PDF 6 point API maps cropped pages at 200% zoom in both directions', () => {
  const view: Viewport = { viewBox: crop, convertToViewportPoint: (x, y) => [2 * x - 20, 240 - 2 * y], convertToPdfPoint: (x, y) => [x / 2 + 10, 120 - y / 2] };
  const screen = annotationToViewport(rect, view);
  expect(screen).toEqual({ left: 100, top: 40, width: 200, height: 60 });
  const result = viewportToAnnotation({ left: 100, top: 40, right: 300, bottom: 100 }, view);
  for (const key of Object.keys(rect)) expect(result[key]).toBeCloseTo(rect[key]);
});
test('90 degree rotation retains the same unrotated PDF coordinates', () => {
  const view: Viewport = { viewBox: crop, convertToViewportPoint: (x, y) => [2 * y - 40, 2 * x - 20], convertToPdfPoint: (x, y) => [(y + 20) / 2, (x + 40) / 2] };
  const screen = annotationToViewport(rect, view);
  expect(screen).toEqual({ left: 100, top: 100, width: 60, height: 200 });
  const result = viewportToAnnotation({ left: 100, top: 100, right: 160, bottom: 300 }, view);
  for (const key of Object.keys(rect)) expect(result[key]).toBeCloseTo(rect[key]);
});
