/*
 * Copyright 2022 Nightingale Team
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */
import React, { useEffect, useState } from 'react';
import { useParams, useRouteMatch } from 'react-router';
import _ from 'lodash';
import { Alert, Card, Affix } from 'antd';
import { useTranslation } from 'react-i18next';
import PageLayout from '@/components/pageLayout';
import { getAlertEventsById, getHistoryEventsById } from '@/services/warning';
import DetailNG from './DetailNG';
import getActions from './DetailNG/Actions';

const EventDetailPage = () => {
  const { t } = useTranslation('AlertCurEvents');
  const { eventId } = useParams<{ eventId: string }>();
  const current = !!useRouteMatch('/alert-cur-events/:eventId');
  const [eventDetail, setEventDetail] = useState<any>();
  const [loadFailed, setLoadFailed] = useState(false);

  useEffect(() => {
    if (!eventId) return;
    let active = true;
    setEventDetail(undefined);
    setLoadFailed(false);
    const load = current ? getAlertEventsById : getHistoryEventsById;
    load(eventId)
      .then((res) => {
        if (!active) return;
        if (res.dat) setEventDetail(res.dat);
        else setLoadFailed(true);
      })
      .catch(() => {
        if (active) setLoadFailed(true);
      });
    return () => {
      active = false;
    };
  }, [eventId, current]);

  return (
    <PageLayout
      title={t('detail.title')}
      showBack
      backPath={current ? undefined : '/alert-his-events'}
      doc='https://flashcat.cloud/docs/content/flashcat-monitor/nightingale-v9/usage/alert-notify/events/alert-history/'
    >
      <div className='n9e'>
        {loadFailed && <Alert className='mb-2' type='error' showIcon message={t('detail.load_failed', { defaultValue: '告警事件加载失败' })} />}
        <Card size='small' className='mb-2'>
          <DetailNG data={eventDetail} showGraph />
        </Card>
        <Affix offsetBottom={0}>
          <Card size='small' className='affix-bottom-shadow'>
            {getActions({
              showAckBtn: true,
              eventDetail: eventDetail,
              showSharingLink: false,
            })}
          </Card>
        </Affix>
      </div>
    </PageLayout>
  );
};

export default EventDetailPage;
