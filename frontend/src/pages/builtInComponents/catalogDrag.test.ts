import { categoryDestination } from './catalogDrag';
jest.mock('@/services/collection', () => ({ collectionRequest: jest.fn() }));
const nodes = [
  { id: 'type', parent_id: '' },
  { id: 'other-type', parent_id: '' },
  { id: 'brand-a', parent_id: 'type' },
  { id: 'brand-b', parent_id: 'type' },
  { id: 'model-a', parent_id: 'brand-a' },
  { id: 'model-b', parent_id: 'brand-b' },
  { id: 'model-c', parent_id: 'brand-b' },
].map((c) => ({ ...c, name: c.id, record_version: 1 }));
test('move a model between brands and a whole brand to another type', () => {
  expect(categoryDestination(nodes, 'model-a', 'brand-b', 0)).toEqual({ parent_id: 'brand-b', before_id: '' });
  expect(categoryDestination(nodes, 'brand-a', 'other-type', 0)).toEqual({ parent_id: 'other-type', before_id: '' });
  expect(categoryDestination(nodes, 'model-a', 'model-b', 1)).toEqual({ parent_id: 'brand-b', before_id: 'model-c' });
  expect(categoryDestination(nodes, 'model-a', 'model-b', -1)).toEqual({ parent_id: 'brand-b', before_id: 'model-b' });
});
test('reorder types but never change levels or nest a parent under a descendant', () => {
  expect(categoryDestination(nodes, 'type', 'other-type', -1)).toEqual({ parent_id: '', before_id: 'other-type' });
  expect(categoryDestination(nodes, 'model-a', 'type', 0)).toBeUndefined();
  expect(categoryDestination(nodes, 'type', 'model-a', 0)).toBeUndefined();
  expect(categoryDestination(nodes, 'brand-a', 'brand-a', 0)).toBeUndefined();
});

test('an empty parent accepts a drop without requiring horizontal indentation', () => {
  expect(categoryDestination(nodes, 'brand-a', 'other-type', 1)).toEqual({ parent_id: 'other-type', before_id: '' });
  expect(categoryDestination(nodes, 'model-a', 'brand-b', 1)).toEqual({ parent_id: 'brand-b', before_id: '' });
});
