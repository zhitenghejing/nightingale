/** @jest-environment jsdom */
jest.mock('@/utils/constant', () => ({ AccessTokenKey: 'access_token' }));

import { filterReports, Report, saveReport } from './api';

const report: Report = {
  id: 'r1',
  kind: 'inspection',
  title: '核心机房巡检',
  engineer: '张工程师',
  company: '运维团队',
  contact: '',
  reportDate: '2026-10-21',
  summary: '检查交换机和服务器',
  filename: 'October.docx',
  extension: '.docx',
  size: 100,
  sha256: '',
  ownerId: 1,
  createdAt: '',
  updatedAt: '',
  version: 1,
  previewStatus: 'pending',
};

afterEach(() => jest.restoreAllMocks());
test('searches title, engineer, summary and filename without changing source order', () => {
  expect(filterReports([report], '张工程师')).toEqual([report]);
  expect(filterReports([report], '交换机')).toEqual([report]);
  expect(filterReports([report], ' OCTOBER ')).toEqual([report]);
  expect(filterReports([report], '不存在')).toEqual([]);
  expect(report.title).toBe('核心机房巡检');
});
test('uploads original and external metadata as multipart and preserves revision on edits', async () => {
  const fetchMock = jest.fn().mockResolvedValue({ ok: true, json: async () => ({ dat: report }) });
  global.fetch = fetchMock;
  localStorage.setItem('access_token', 'test-token');
  const file = new File(['document'], 'October.docx');
  await saveReport('inspection', report, file);
  const options = fetchMock.mock.calls[0][1];
  expect(options.body).toBeInstanceOf(FormData);
  expect(options.body.get('file')).toBe(file);
  expect(JSON.parse(options.body.get('metadata')).engineer).toBe('张工程师');
  expect(options.headers.Authorization).toBe('Bearer test-token');
  await saveReport('inspection', { ...report, summary: '更新摘要' }, undefined, report);
  expect(JSON.parse(fetchMock.mock.calls[1][1].body).version).toBe(1);
  expect(fetchMock.mock.calls[1][1].method).toBe('PUT');
});
