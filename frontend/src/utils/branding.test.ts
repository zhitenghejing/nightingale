import { BRAND_ASSETS, getMenuLogoSrc } from './branding';

describe('menu brand selection', () => {
  it('adapts the expanded wordmark to the existing menu background', () => {
    expect(getMenuLogoSrc(false, 'light')).toBe(BRAND_ASSETS.light);
    expect(getMenuLogoSrc(false, 'dark')).toBe(BRAND_ASSETS.dark);
    expect(getMenuLogoSrc(false, 'theme')).toBe(BRAND_ASSETS.dark);
  });

  it('keeps the collapsed symbol readable on each background', () => {
    for (const tone of ['light', 'dark', 'theme']) {
      expect(getMenuLogoSrc(true, tone)).toBe(BRAND_ASSETS.mark);
    }
  });

  it('preserves site and downstream overrides independently for each slot', () => {
    const defaults = { light_menu_big_logo_url: '/downstream-light.svg', menu_big_logo_url: '/downstream-dark.svg' };
    const site = { light_menu_big_logo_url: '/custom-light.svg', menu_small_logo_url: '/custom-mark.svg' };
    expect(getMenuLogoSrc(false, 'light', defaults, site)).toBe('/custom-light.svg');
    expect(getMenuLogoSrc(false, 'dark', defaults, site)).toBe('/downstream-dark.svg');
    expect(getMenuLogoSrc(true, 'dark', defaults, site)).toBe('/custom-mark.svg');
    expect(getMenuLogoSrc(true, 'light', defaults, site)).toBe(BRAND_ASSETS.mark);
    expect(getMenuLogoSrc(false, 'light', defaults, { light_menu_big_logo_url: '' })).toBe('/downstream-light.svg');
  });
});
