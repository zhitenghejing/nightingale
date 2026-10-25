import request from '@/utils/request';
import { api } from './api';
jest.mock('@/utils/request', () => ({ __esModule: true, default: jest.fn() }));
const mocked = request as jest.Mock;
beforeEach(() => jest.clearAllMocks());
it('preserves the backend explanation from HTTP validation rejections', async () => {
  mocked.mockRejectedValueOnce({ status: 400, message: '机房下还有机柜，请先迁移或删除机柜' });
  await expect(api('rooms/room', 'DELETE')).rejects.toThrow('机房下还有机柜，请先迁移或删除机柜');
});
it('keeps versioned DELETE data and rejects invalid response envelopes', async () => {
  mocked.mockResolvedValueOnce({ dat: true, err: '' });
  await expect(api('racks/rack', 'DELETE', { version: 2 })).resolves.toBe(true);
  expect(mocked).toHaveBeenCalledWith('/api/cmdb/racks/rack', { method: 'DELETE', data: { version: 2 }, silence: true });
  mocked.mockResolvedValueOnce('<html>not an API</html>');
  await expect(api('catalog')).rejects.toThrow('服务未返回有效结果');
});
