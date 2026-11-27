/** Product identity. Keep routes, API identifiers and third-party brands unchanged. */
export const PRODUCT_NAME = '致腾慧运维';
export const PRODUCT_AI_NAME = '致慧AI';

export const BRAND_ASSETS = {
  light: '/image/brand/logo-light.svg',
  dark: '/image/brand/logo-dark.svg',
  mark: '/image/brand/mark.svg',
} as const;

export const DEFAULT_MENU_LOGOS = {
  light_menu_big_logo_url: BRAND_ASSETS.light,
  light_menu_small_logo_url: BRAND_ASSETS.mark,
  menu_big_logo_url: BRAND_ASSETS.dark,
  menu_small_logo_url: BRAND_ASSETS.mark,
};

type MenuLogos = Partial<Record<keyof typeof DEFAULT_MENU_LOGOS, string>>;

/** Existing site customizations and downstream defaults retain their priority. */
export function getMenuLogoSrc(collapsed: boolean, sideMenuBgMode: string, defaults: MenuLogos = {}, siteInfo?: MenuLogos): string {
  const key = sideMenuBgMode === 'light' ? (collapsed ? 'light_menu_small_logo_url' : 'light_menu_big_logo_url') : collapsed ? 'menu_small_logo_url' : 'menu_big_logo_url';
  return siteInfo?.[key] || defaults[key] || DEFAULT_MENU_LOGOS[key];
}
