/** @jest-environment jsdom */
import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import RackViewport from './RackViewport';

it('zooms with the wheel only within the viewport, clamps limits, without a reset button', () => {
  render(
    <RackViewport>
      <button>设备</button>
    </RackViewport>,
  );
  const viewport = screen.getByRole('region', { name: '机柜图缩放区域' });
  const percentage = screen.getByLabelText('机柜图缩放比例');
  const wheel = new WheelEvent('wheel', { deltaY: -100, bubbles: true, cancelable: true });
  fireEvent(viewport, wheel);
  expect(wheel.defaultPrevented).toBe(true);
  expect(percentage.textContent).toBe('122%');
  const outside = new WheelEvent('wheel', { deltaY: 100, bubbles: true, cancelable: true });
  fireEvent(document.body, outside);
  expect(outside.defaultPrevented).toBe(false);
  expect(percentage.textContent).toBe('122%');
  for (let i = 0; i < 30; i++) fireEvent.wheel(viewport, { deltaY: -100 });
  expect(percentage.textContent).toBe('200%');
  expect(screen.getByRole('button', { name: '放大机柜图' })).toBeDisabled();
  for (let i = 0; i < 30; i++) fireEvent.wheel(viewport, { deltaY: 100 });
  expect(percentage.textContent).toBe('2%');
  expect(screen.getByRole('button', { name: '缩小机柜图' })).toBeDisabled();
  expect(screen.queryByRole('button', { name: '还原 100%' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: '设备' }));
});

it('keeps the point under the mouse anchored while zooming', () => {
  render(
    <RackViewport>
      <div>机柜</div>
    </RackViewport>,
  );
  const viewport = screen.getByRole('region', { name: '机柜图缩放区域' });
  Object.defineProperty(viewport, 'clientWidth', { value: 800 });
  Object.defineProperty(viewport, 'clientHeight', { value: 600 });
  viewport.scrollTop = 200;
  fireEvent.wheel(viewport, { deltaY: -100, clientX: 400, clientY: 150 });
  expect(viewport.scrollTop).toBeCloseTo((200 + 150 - 24) * Math.exp(0.2) + 24 - 150);
  expect(viewport.scrollLeft).toBeCloseTo(0);
});

it('fits the cabinet to the viewport and releases the wheel listener on unmount', () => {
  const height = jest.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(1400);
  const view = render(
    <RackViewport>
      <div>机柜</div>
    </RackViewport>,
  );
  const viewport = screen.getByRole('region', { name: '机柜图缩放区域' });
  Object.defineProperty(viewport, 'clientWidth', { value: 800 });
  Object.defineProperty(viewport, 'clientHeight', { value: 600 });
  fireEvent.click(screen.getByRole('button', { name: '适应窗口' }));
  expect(screen.getByLabelText('机柜图缩放比例').textContent).toBe('39%');
  view.unmount();
  const wheel = new WheelEvent('wheel', { deltaY: 100, cancelable: true });
  viewport.dispatchEvent(wheel);
  expect(wheel.defaultPrevented).toBe(false);
  height.mockRestore();
});

it('fits initially and on resize, preserves manual zoom until Fit Window is chosen', () => {
  let width = 800;
  const height = jest.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(1400);
  const clientWidth = jest.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => width);
  const clientHeight = jest.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(600);
  const originalObserver = global.ResizeObserver;
  let resize: () => void = () => {};
  global.ResizeObserver = class {
    constructor(callback: () => void) {
      resize = callback;
    }
    observe() {}
    unobserve() {}
    disconnect() {}
  } as any;
  try {
    render(
      <RackViewport>
        <div>机柜</div>
      </RackViewport>,
    );
    const percentage = screen.getByLabelText('机柜图缩放比例');
    expect(percentage.textContent).toBe('39%');
    width = 240;
    act(() => resize());
    expect(percentage.textContent).toBe('33%');
    fireEvent.click(screen.getByRole('button', { name: '放大机柜图' }));
    const manual = percentage.textContent;
    width = 800;
    act(() => resize());
    expect(percentage.textContent).toBe(manual);
    fireEvent.click(screen.getByRole('button', { name: '适应窗口' }));
    expect(percentage.textContent).toBe('39%');
  } finally {
    global.ResizeObserver = originalObserver;
    height.mockRestore();
    clientWidth.mockRestore();
    clientHeight.mockRestore();
  }
});

it('fits a 60U cabinet in a short window below ten percent', () => {
  const height = jest.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(3000);
  const width = jest.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(340);
  const viewportHeight = jest.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(200);
  try {
    render(
      <RackViewport>
        <div>60U</div>
      </RackViewport>,
    );
    expect(screen.getByLabelText('机柜图缩放比例').textContent).toBe('5%');
  } finally {
    height.mockRestore();
    width.mockRestore();
    viewportHeight.mockRestore();
  }
});
