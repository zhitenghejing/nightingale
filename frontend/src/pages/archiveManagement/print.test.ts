/** @jest-environment jsdom */
import { openPrintWindow, printPdf } from './print';
import { renderPrintPages } from './printRenderer';
import { waitFor } from '@testing-library/dom';
jest.mock('./printRenderer', () => ({ renderPrintPages: jest.fn() }));

afterEach(() => {
  jest.restoreAllMocks();
  jest.mocked(renderPrintPages).mockReset();
  document.body.innerHTML = '';
});

function popupWindow() {
  const frame = document.createElement('iframe');
  document.body.appendChild(frame);
  const popup = frame.contentWindow!;
  jest.spyOn(window, 'open').mockReturnValue(popup);
  jest.spyOn(popup, 'focus').mockImplementation(() => undefined);
  jest.spyOn(popup, 'print').mockImplementation(() => undefined);
  return popup;
}

test('printing waits for rendered pages and audit, then offers manual retry without navigating to PDF', async () => {
  const popup = popupWindow();
  let rendered!: () => void;
  let audited!: () => void;
  jest.mocked(renderPrintPages).mockImplementation(
    () =>
      new Promise((resolve) => {
        rendered = resolve;
      }),
  );
  const audit = jest.fn(
    () =>
      new Promise<void>((resolve) => {
        audited = resolve;
      }),
  );
  expect(openPrintWindow()).toBe(popup);
  const location = popup.location.href;
  const button = popup.document.querySelector('button')!;
  const pending = printPdf(popup, new Blob(), audit);
  await waitFor(() => expect(renderPrintPages).toHaveBeenCalled());
  expect(button.disabled).toBe(true);
  expect(popup.print).not.toHaveBeenCalled();
  expect(audit).not.toHaveBeenCalled();
  rendered();
  await waitFor(() => expect(audit).toHaveBeenCalledTimes(1));
  expect(popup.print).not.toHaveBeenCalled();
  audited();
  await pending;
  expect(popup.print).toHaveBeenCalledTimes(1);
  expect(popup.location.href).toBe(location);
  expect(button.disabled).toBe(false);
  audit.mockResolvedValue(undefined);
  button.click();
  await waitFor(() => expect(popup.print).toHaveBeenCalledTimes(2));
  expect(audit).toHaveBeenCalledTimes(2);
});

test('blocked popups report the reason before document generation', () => {
  jest.spyOn(window, 'open').mockReturnValue(null);
  expect(openPrintWindow).toThrow('浏览器阻止了打印窗口');
  expect(renderPrintPages).not.toHaveBeenCalled();
});

test('failed rendering does not print or write a print log', async () => {
  const popup = popupWindow();
  openPrintWindow();
  jest.mocked(renderPrintPages).mockRejectedValue(new Error('PDF damaged'));
  const audit = jest.fn();
  await expect(printPdf(popup, new Blob(), audit)).rejects.toThrow('PDF damaged');
  expect(popup.print).not.toHaveBeenCalled();
  expect(audit).not.toHaveBeenCalled();
});

test('a rejected automatic print keeps the rendered preview for manual retry', async () => {
  const popup = popupWindow();
  openPrintWindow();
  jest.mocked(renderPrintPages).mockResolvedValue(undefined);
  jest.mocked(popup.print).mockImplementationOnce(() => {
    throw new Error('Automatic print blocked');
  });
  await printPdf(popup, new Blob(), jest.fn().mockResolvedValue(undefined));
  expect(popup.document.querySelector('[role=status]')!.textContent).toContain('未能唤起');
  popup.document.querySelector('button')!.click();
  await waitFor(() => expect(popup.print).toHaveBeenCalledTimes(2));
});
