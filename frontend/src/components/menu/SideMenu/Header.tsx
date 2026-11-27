import React, { useContext } from 'react';
import { cn } from '@/components/menu/SideMenu/utils';
import { CommonStateContext } from '@/App';
import { PRODUCT_NAME, getMenuLogoSrc } from '@/utils/branding';

interface Props {
  collapsed: boolean;
  collapsedHover: boolean;
  sideMenuBgMode: string;
}

export default function SideMenuHeader(props: Props) {
  const { collapsed, collapsedHover, sideMenuBgMode } = props;
  const { siteInfo } = useContext(CommonStateContext);

  const noCollapsedLogo = getMenuLogoSrc(false, sideMenuBgMode, {}, siteInfo);
  const collapsedLogo = getMenuLogoSrc(true, sideMenuBgMode, {}, siteInfo);
  const showExpandedLogo = !collapsed || collapsedHover;

  return (
    <div className={cn('relative mt-6 h-10 w-full shrink-0 overflow-hidden transition-spacing flex', showExpandedLogo ? 'justify-start pl-2' : 'justify-center')}>
      <div className={cn(showExpandedLogo && 'pl-3.5')}>
        <img
          src={noCollapsedLogo}
          alt={PRODUCT_NAME}
          width={120}
          height={38}
          className='max-w-[120px]'
          style={{
            display: showExpandedLogo ? 'block' : 'none',
          }}
        />
        <img
          src={collapsedLogo}
          alt={PRODUCT_NAME}
          width={36}
          height={38}
          className='max-w-[120px]'
          style={{
            display: showExpandedLogo ? 'none' : 'block',
          }}
        />
      </div>
    </div>
  );
}
