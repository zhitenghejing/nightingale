import React from 'react';
import { Drawer, Space } from 'antd';
import { CloseOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';

import DetailNG from '@/pages/event/DetailNG';
import getActions from '@/pages/event/DetailNG/Actions';
import EnterOpsCaseButton from '@/pages/opsShared/EnterOpsCaseButton';

import { NS } from '../../constants';

export interface Props {
  showDeleteBtn?: boolean;
  showAckBtn?: boolean;
  visible: boolean;
  data: any;
  onClose: () => void;
  onDeleteSuccess: () => void;
  onRefresh?: () => void;
}

export default function EventDetailDrawer(props: Props) {
  const { t } = useTranslation(NS);
  const { showDeleteBtn = true, showAckBtn, visible, onClose, data, onDeleteSuccess, onRefresh } = props;

  return (
    <Drawer
      width='80%'
      closable={false}
      title={t('detail_title')}
      destroyOnClose
      extra={<CloseOutlined onClick={() => onClose()} />}
      onClose={() => onClose()}
      visible={visible}
      footer={
        <Space wrap>
          <EnterOpsCaseButton eventId={data?.id} onEntered={onClose} />
          {getActions({
            eventDetail: data,
            showDeleteBtn,
            showAckBtn,
            onDeleteSuccess: () => {
              onClose();
              onDeleteSuccess();
            },
            onRefresh,
          })}
        </Space>
      }
      className='n9e-antd-drawer'
    >
      {data && <DetailNG data={data} showGraph />}
    </Drawer>
  );
}
