import React, { useContext } from 'react';
import { Tooltip } from 'antd';
import { PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import { cn } from '@/components/menu/SideMenu/utils';
import { CommonStateContext } from '@/App';
import { BRAND_ASSETS, PRODUCT_NAME, getMenuLogoSrc } from '@/utils/branding';

import { DefaultLogos } from './types';

const lightCollapseButtonClass = 'text-[var(--fc-sidemenu-item-icon)] hover:bg-[var(--fc-sidemenu-item-hover-bg)] hover:text-[var(--fc-sidemenu-item-hover-text)]';

interface Props {
  collapsed: boolean;
  sideMenuBgMode: string;
  defaultLogos: DefaultLogos;
  onToggleCollapse: () => void;
  toggleTitle: string;
}

export default function SideMenuHeader(props: Props) {
  const { collapsed, sideMenuBgMode, defaultLogos, onToggleCollapse, toggleTitle } = props;
  const { siteInfo } = useContext(CommonStateContext);

  const noCollapsedLogo = getMenuLogoSrc(false, sideMenuBgMode, defaultLogos, siteInfo);
  const collapsedLogo = getMenuLogoSrc(true, sideMenuBgMode, defaultLogos, siteInfo);
  const showProductName = noCollapsedLogo === BRAND_ASSETS.light || noCollapsedLogo === BRAND_ASSETS.dark;

  if (collapsed) {
    return (
      <div className='side-menu-collapsed-logo-row relative flex h-[50px] w-full shrink-0 items-center justify-center px-2'>
        <Tooltip title={toggleTitle} placement='right'>
          <button
            type='button'
            aria-label={toggleTitle}
            className={cn(
              'side-menu-collapsed-logo-toggle flex h-8 w-8 shrink-0 cursor-pointer items-center justify-center rounded border-0 bg-transparent p-0 transition-colors',
              sideMenuBgMode === 'light' ? lightCollapseButtonClass : 'text-[#fff] hover:bg-gray-200/20',
            )}
            onClick={onToggleCollapse}
          >
            <img src={collapsedLogo} alt={PRODUCT_NAME} width={24} height={24} className='side-menu-collapsed-logo h-6 w-6 object-contain' />
            <PanelLeftOpen className='side-menu-collapsed-logo-icon' size={16} strokeWidth={1.8} />
          </button>
        </Tooltip>
      </div>
    );
  }

  return (
    <div className='side-menu-logo-row relative flex h-[50px] w-full shrink-0 items-center gap-2 overflow-hidden pl-5 pr-2 transition-spacing'>
      <div className={cn('min-w-0 flex-1 flex items-center gap-1.5', sideMenuBgMode === 'light' ? 'text-[#202631]' : 'text-white')}>
        <img
          src={showProductName ? BRAND_ASSETS.mark : noCollapsedLogo}
          alt={PRODUCT_NAME}
          width={showProductName ? 24 : 96}
          height={28}
          className={cn('block max-h-7 shrink-0 object-contain', showProductName ? 'w-6' : 'max-w-[96px]')}
        />
        {showProductName && (
          <>
            <span className='h-5 w-px shrink-0 bg-current opacity-20' aria-hidden='true' />
            <span className='whitespace-nowrap text-[12px] font-semibold'>{PRODUCT_NAME}</span>
          </>
        )}
      </div>
      <Tooltip title={toggleTitle} placement='right'>
        <button
          type='button'
          aria-label={toggleTitle}
          className={cn(
            'side-menu-header-collapse-button flex h-7 w-5 shrink-0 cursor-pointer items-center justify-center rounded border-0 bg-transparent p-0 transition-colors',
            sideMenuBgMode === 'light' ? lightCollapseButtonClass : 'text-[#fff]/70 hover:bg-gray-200/20 hover:text-[#fff]',
          )}
          onClick={onToggleCollapse}
        >
          <PanelLeftClose size={16} strokeWidth={1.8} />
        </button>
      </Tooltip>
    </div>
  );
}
