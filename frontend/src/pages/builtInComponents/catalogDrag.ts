import type { Category } from './catalog';
import { categoryPath } from './catalog';

export const TEMPLATE_DRAG_TYPE = 'application/x-n9e-template';
// Position 0 means inside the target; -1/+1 mean before/after its row.
export function categoryDestination(categories: Category[], sourceId: string, targetId: string, position: number) {
  if (sourceId === targetId) return;
  const source = categories.find((c) => c.id === sourceId),
    target = categories.find((c) => c.id === targetId);
  if (!source || !target) return;
  // rc-tree reports "after" for an empty parent unless the pointer is indented.
  // Semantic levels make this unambiguous: dropping on the immediately higher
  // level always means moving inside it, even when the library reports a gap.
  if (categoryPath(categories, source.id).length === categoryPath(categories, target.id).length + 1) return { parent_id: target.id, before_id: '' };
  const parent_id = position === 0 ? target.id : target.parent_id;
  if (categoryPath(categories, source.id).length !== categoryPath(categories, parent_id).length + 1) return;
  const siblings = categories.filter((c) => c.parent_id === parent_id && c.id !== sourceId);
  const before_id = position < 0 ? target.id : position > 0 ? siblings[siblings.findIndex((c) => c.id === target.id) + 1]?.id || '' : '';
  return { parent_id, before_id };
}
