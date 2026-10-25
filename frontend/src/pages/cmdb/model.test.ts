import { blankDevice, csvRows, deviceError, exportCsv, importCsv, sampleDevices, warrantyStatus } from './model';

describe('CMDB device ledger', () => {
  it('distinguishes warranty dates from monitoring state', () => {
    expect(warrantyStatus('2026-10-20', '2026-10-21')).toBe('已过保');
    expect(warrantyStatus('2026-11-20', '2026-10-21')).toBe('即将过保');
    expect(warrantyStatus('2026-11-21', '2026-10-21')).toBe('在保');
    expect(warrantyStatus('', '2026-10-21')).toBe('未填写');
  });

  it('rejects overlapping rack positions but permits the same U range in another cabinet', () => {
    const devices = sampleDevices();
    const value = { ...devices[0], id: 'new', serial: 'NEW', uStart: 10, uEnd: 11 };
    expect(deviceError(value, devices)).toContain('冲突');
    expect(deviceError({ ...value, rack: '新机柜' }, devices)).toBeUndefined();
    expect(deviceError(devices[0], devices)).toBeUndefined();
  });

  it('rejects duplicate serials and inconsistent warranty dates', () => {
    const devices = sampleDevices();
    expect(deviceError({ ...devices[0], id: 'new', serial: 'demo-lnv-001' }, devices)).toBe('序列号已存在');
    expect(deviceError({ ...devices[0], warrantyStart: '2030-01-01' }, devices)).toContain('不能晚于');
  });

  it('round-trips quoted values, monitoring switches and all disk groups', () => {
    const device = { ...sampleDevices()[0], note: '设备,"双引号"\n第二行', monitored: true, alertEnabled: true };
    const [restored] = importCsv(exportCsv([device]), []);
    expect(restored.note).toBe(device.note);
    expect(restored.disks).toEqual(device.disks);
    expect(restored.monitored).toBe(true);
    expect(restored.alertEnabled).toBe(true);
    expect(restored.monitorState).toBe('等待心跳');
  });

  it('rejects a batch with duplicate serial numbers without modifying existing records', () => {
    const existing = sampleDevices();
    const original = JSON.stringify(existing);
    expect(() => importCsv(exportCsv([existing[0]]), existing)).toThrow('序列号已存在');
    expect(() => importCsv(exportCsv([existing[0], existing[0]]), [])).toThrow('序列号已存在');
    expect(JSON.stringify(existing)).toBe(original);
  });

  it('does not enable alerts for an imported device without monitoring', () => {
    const device = { ...blankDevice, customer: '医院', hostname: '测试', serial: 'NEW', monitored: false, alertEnabled: true };
    expect(importCsv(exportCsv([device]), [])[0].alertEnabled).toBe(false);
    expect(() => csvRows('"未闭合')).toThrow('引号未闭合');
  });
});
