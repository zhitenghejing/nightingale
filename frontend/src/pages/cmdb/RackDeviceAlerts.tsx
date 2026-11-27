import React from 'react';
import { Alert, Table, Tag } from 'antd';
import moment from 'moment';
import { Link } from 'react-router-dom';
import { RackAlert } from './api';

export const rackAlertLevels = {
  1: { label: '一级告警', color: 'red' },
  2: { label: '二级告警', color: 'orange' },
  3: { label: '三级告警', color: 'gold' },
};

export default function RackDeviceAlerts({ alerts, failed }: { alerts: RackAlert[]; failed: boolean }) {
  return (
    <section className='cmdb-device-alerts' aria-label='设备当前告警'>
      <h3>设备当前告警</h3>
      {failed ? (
        <Alert type='warning' showIcon message='当前告警暂不可用' />
      ) : alerts.length ? (
        <Table<RackAlert>
          size='small'
          rowKey='id'
          pagination={alerts.length > 8 ? { pageSize: 8, showSizeChanger: false } : false}
          dataSource={alerts}
          columns={[
            {
              title: '告警级别',
              width: 115,
              render: (_, { severity }) => <Tag color={rackAlertLevels[severity].color}>{rackAlertLevels[severity].label}</Tag>,
            },
            {
              title: '告警事件',
              render: (_, event) => <Link to={`/alert-cur-events/${event.id}`}>{event.rule_name || `告警 ${event.id}`}</Link>,
            },
            { title: '触发时间', width: 180, render: (_, event) => (event.trigger_time ? moment.unix(event.trigger_time).format('YYYY-MM-DD HH:mm:ss') : '—') },
          ]}
        />
      ) : (
        <span className='text-fc-text-4'>暂无当前告警</span>
      )}
    </section>
  );
}
