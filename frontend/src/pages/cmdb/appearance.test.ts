import { appearance } from './appearance';
import { blankDevice } from './model';
it('matches the model and falls back without changing asset height', () => {
  const d = { ...blankDevice, type: '机架式服务器', vendor: 'DELL', model: 'PowerEdge R650', uStart: 1, uEnd: 1 };
  expect(appearance(d)).toEqual({ kind: 'server', brand: 'dell' });
  expect(appearance({ ...d, uEnd: 2 })).toEqual({ kind: 'generic', brand: 'generic' });
  expect(appearance({ ...d, model: 'Unknown model' })).toEqual({ kind: 'generic', brand: 'generic' });
});
