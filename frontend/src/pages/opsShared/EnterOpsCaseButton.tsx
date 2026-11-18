import React, { useRef, useState } from 'react';
import { Button, message } from 'antd';
import { useHistory } from 'react-router-dom';
import { api, errorText, OpsCase } from './api';

export default function EnterOpsCaseButton({ eventId, onEntered }: { eventId?: number; onEntered?: () => void }) {
  const history = useHistory();
  const [loading, setLoading] = useState(false);
  const pending = useRef(false);

  return (
    <Button
      loading={loading}
      disabled={!eventId || !Number.isSafeInteger(eventId) || eventId <= 0}
      onClick={async () => {
        if (pending.current) return;
        pending.current = true;
        setLoading(true);
        try {
          const row = await api<OpsCase>('/cases', 'POST', { event_id: eventId });
          onEntered?.();
          history.push(`/ops/cases/${row.id}`);
        } catch (e) {
          message.error(errorText(e));
        } finally {
          pending.current = false;
          setLoading(false);
        }
      }}
    >
      进入处置闭环
    </Button>
  );
}
