/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { message } from 'antd';
import LogoPicker from './index';
import { Component } from '../../services';
import { addTemplateIcon, getTemplateIcons } from '../../catalog';
import { CommonStateContext } from '@/App';

jest.mock('@/App', () => ({ CommonStateContext: require('react').createContext({ profile: { admin: true } }) }));
jest.mock('../../catalog', () => ({ getTemplateIcons: jest.fn(), addTemplateIcon: jest.fn() }));
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
const icon = { id: 'new-icon', name: 'Device.svg', url: '/api/n9e/collection/icons/new-icon/content', created_at: 1 };
const components: Component[] = [{ id: 1, ident: 'Windows', logo: '/windows.svg', readme: '', disabled: 0 }];
const open = async () => {
  fireEvent.click(screen.getByText('Open picker'));
  await waitFor(() => expect(getTemplateIcons).toHaveBeenCalled());
  await waitFor(() => expect(screen.getByRole('button', { name: 'logo_add_material' })).toBeEnabled());
};
const chooseFile = (file: File) => fireEvent.change(screen.getByLabelText('logo_add_material', { selector: 'input' }), { target: { files: [file] } });

beforeEach(() => {
  (getTemplateIcons as jest.Mock).mockResolvedValue([]);
  (addTemplateIcon as jest.Mock).mockReset();
  jest.spyOn(message, 'success').mockImplementation(jest.fn());
  jest.spyOn(message, 'error').mockImplementation(jest.fn());
});
afterEach(() => jest.restoreAllMocks());

test('imports reusable artwork, selects its URL, and reloads the library when reopened', async () => {
  (addTemplateIcon as jest.Mock).mockResolvedValue(icon);
  const onSelect = jest.fn();
  render(
    <LogoPicker components={components} onSelect={onSelect}>
      Open picker
    </LogoPicker>,
  );
  await open();
  expect(screen.getByRole('button', { name: 'Windows' })).toBeVisible();
  chooseFile(new File(['<svg/>'], 'Device.svg', { type: 'image/svg+xml' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Device.svg' })).toBeVisible());
  expect(addTemplateIcon).toHaveBeenCalledWith('Device.svg', expect.stringContaining('data:image/svg+xml;base64,'));
  expect(onSelect).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Device.svg' }));
  expect(onSelect).toHaveBeenCalledWith(icon.url);
  (getTemplateIcons as jest.Mock).mockResolvedValue([icon]);
  await open();
  await waitFor(() => expect(getTemplateIcons).toHaveBeenCalledTimes(2));
  expect(screen.getAllByRole('button', { name: 'Device.svg' })).toHaveLength(1);
});

test('rejects invalid local files and recovers from upload failure without closing the picker', async () => {
  render(
    <LogoPicker components={components} onSelect={jest.fn()}>
      Open picker
    </LogoPicker>,
  );
  await open();
  chooseFile(new File(['html'], 'bad.html', { type: 'text/html' }));
  chooseFile(new File([new Uint8Array(2 * 1024 * 1024 + 1)], 'large.png', { type: 'image/png' }));
  expect(addTemplateIcon).not.toHaveBeenCalled();
  (addTemplateIcon as jest.Mock).mockRejectedValueOnce(new Error('Import failed')).mockResolvedValue(icon);
  chooseFile(new File(['<svg/>'], 'Device.svg', { type: 'image/svg+xml' }));
  await waitFor(() => expect(message.error).toHaveBeenCalledWith('Import failed'));
  await waitFor(() => expect(screen.getByRole('button', { name: 'logo_add_material' })).toBeEnabled());
  chooseFile(new File(['<svg/>'], 'Device.svg', { type: 'image/svg+xml' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Device.svg' })).toBeVisible());
});

test('library load failure is retryable and native icons remain selectable', async () => {
  (getTemplateIcons as jest.Mock).mockRejectedValueOnce(new Error('Unavailable')).mockResolvedValue([icon]);
  const onSelect = jest.fn();
  render(
    <LogoPicker components={components} onSelect={onSelect}>
      Open picker
    </LogoPicker>,
  );
  await open();
  fireEvent.click(screen.getByRole('button', { name: 'logo_retry' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Device.svg' })).toBeVisible());
  fireEvent.click(screen.getByRole('button', { name: 'Windows' }));
  expect(onSelect).toHaveBeenCalledWith('/windows.svg');
});

test('readers can reuse uploaded materials without an upload control', async () => {
  (getTemplateIcons as jest.Mock).mockResolvedValue([icon]);
  const onSelect = jest.fn();
  render(
    <CommonStateContext.Provider value={{ profile: { admin: false } } as any}>
      <LogoPicker components={components} onSelect={onSelect}>
        Open picker
      </LogoPicker>
    </CommonStateContext.Provider>,
  );
  fireEvent.click(screen.getByText('Open picker'));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Device.svg' })).toBeVisible());
  expect(screen.queryByRole('button', { name: 'logo_add_material' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Device.svg' }));
  expect(onSelect).toHaveBeenCalledWith(icon.url);
});

test('legacy picker works with collection disabled and no inherited app context', async () => {
  (getTemplateIcons as jest.Mock).mockClear();
  const onSelect = jest.fn();
  render(
    <CommonStateContext.Provider value={{} as any}>
      <LogoPicker libraryEnabled={false} components={components} onSelect={onSelect}>
        Open picker
      </LogoPicker>
    </CommonStateContext.Provider>,
  );
  fireEvent.click(screen.getByText('Open picker'));
  fireEvent.click(await screen.findByRole('button', { name: 'Windows' }));
  expect(onSelect).toHaveBeenCalledWith('/windows.svg');
  expect(getTemplateIcons).not.toHaveBeenCalled();
});
