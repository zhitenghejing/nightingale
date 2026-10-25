import { attachMaterial, matchMaterial, searchMaterials, materials } from './materialCatalog';
import { blankDevice } from './model';
const base = { ...blankDevice, vendor: 'Dell', model: 'R750', uStart: 9, uEnd: 10 };
it('matches a complete model with a known vendor alias and normalized spacing', () => {
  expect(matchMaterial({ ...base, vendor: '戴尔', model: 'Dell EMC PowerEdge R750' }).material?.id).toBe('dell-r750');
  expect(matchMaterial({ ...base, vendor: '惠普', model: 'HP DL380 G10' }).material?.id).toBe('hpe-dl380-gen10');
  expect(matchMaterial({ ...base, vendor: 'HPE 惠普', model: 'HPE ProLiant DL380 G10' }).material?.id).toBe('hpe-dl380-gen10');
  expect(matchMaterial({ ...base, vendor: 'Lenovo', model: 'ThinkSystem SR650_V2' }).material?.id).toBe('lenovo-sr650-v2');
});
it('keeps different generations, suffixes, vendor names and device types separate', () => {
  for (const model of ['R750xs', 'R750xa', 'R740xd']) expect(matchMaterial({ ...base, model }).status).toBe('unmatched');
  expect(matchMaterial({ ...base, vendor: 'HPE', model: 'DL380 Gen10 Plus' }).status).toBe('unmatched');
  expect(matchMaterial({ ...base, vendor: 'HPE', model: 'DL380 Gen11' }).material?.id).toBe('hpe-dl380-gen11');
  expect(matchMaterial({ ...base, vendor: 'HPE', model: 'R750' }).status).toBe('unmatched');
  expect(matchMaterial({ ...base, type: '交换机' }).status).toBe('unmatched');
});
it('validates the selected ID and physical height instead of stretching another model', () => {
  expect(matchMaterial({ ...base, assetMaterialId: 'dell-r740' }).status).toBe('conflict');
  expect(matchMaterial({ ...base, model: 'R650' }).status).toBe('height-mismatch');
  expect(() => attachMaterial({ ...base, model: 'R650' })).toThrow('1U');
  expect(attachMaterial({ ...base, model: 'R650', uEnd: 9 }).assetMaterialId).toBe('dell-r650');
  expect(attachMaterial({ ...base, uStart: undefined, uEnd: undefined }).assetMaterialId).toBe('dell-r750');
  expect(attachMaterial(base).assetMaterialId).toBe('dell-r750');
  expect(base).not.toHaveProperty('assetMaterialId');
});
it('distinguishes the exact H3C switch from PoE, SI and later revisions', () => {
  const device = { ...blankDevice, type: '交换机', vendor: '新华三', model: 'LS-5130S-28S-EI-GL', uStart: 1, uEnd: 1 };
  expect(matchMaterial(device).material?.id).toBe('h3c-s5130s-28s-ei');
  for (const model of ['S5130S-28S-PWR-EI', 'S5130S-28S-SI', 'S5130S-28S-EI-R']) expect(matchMaterial({ ...device, model }).status).toBe('unmatched');
});

it('finds a model by brand and short generation aliases in selectors', () => {
  const result = materials.filter((m) => searchMaterials('惠普 DL380 G10', m));
  expect(result.map((m) => m.id)).toEqual(['hpe-dl380-gen10']);
});
