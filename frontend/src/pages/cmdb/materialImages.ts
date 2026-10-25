import material0front from './materials/panels/hpe-dl380-gen10-front.svg';
import material0rear from './materials/panels/hpe-dl380-gen10-rear.svg';
import material1front from './materials/panels/hpe-dl360-gen10-front.svg';
import material1rear from './materials/panels/hpe-dl360-gen10-rear.svg';
import material2front from './materials/panels/hpe-dl380-gen11-front.svg';
import material2rear from './materials/panels/hpe-dl380-gen11-rear.svg';
import material3front from './materials/panels/dell-r740-front.svg';
import material3rear from './materials/panels/dell-r740-rear.svg';
import material4front from './materials/panels/dell-r750-front.svg';
import material4rear from './materials/panels/dell-r750-rear.svg';
import material5front from './materials/panels/dell-r650-front.svg';
import material5rear from './materials/panels/dell-r650-rear.svg';
import material6front from './materials/panels/lenovo-sr650-v2-front.svg';
import material6rear from './materials/panels/lenovo-sr650-v2-rear.svg';
import material7front from './materials/panels/huawei-2288h-v5-front.svg';
import material7rear from './materials/panels/huawei-2288h-v5-rear.svg';
import material8front from './materials/panels/inspur-nf5280m6-front.svg';
import material8rear from './materials/panels/inspur-nf5280m6-rear.svg';
import material9front from './materials/panels/h3c-s5130s-28s-ei-front.svg';
import material9rear from './materials/panels/h3c-s5130s-28s-ei-rear.svg';

export const materialImages: Record<string, { front: string; rear: string }> = {
  'hpe-dl380-gen10': { front: material0front, rear: material0rear },
  'hpe-dl360-gen10': { front: material1front, rear: material1rear },
  'hpe-dl380-gen11': { front: material2front, rear: material2rear },
  'dell-r740': { front: material3front, rear: material3rear },
  'dell-r750': { front: material4front, rear: material4rear },
  'dell-r650': { front: material5front, rear: material5rear },
  'lenovo-sr650-v2': { front: material6front, rear: material6rear },
  'huawei-2288h-v5': { front: material7front, rear: material7rear },
  'inspur-nf5280m6': { front: material8front, rear: material8rear },
  'h3c-s5130s-28s-ei': { front: material9front, rear: material9rear },
};
