#!/usr/bin/env python3
"""Exercise label mapping and alert-rule-controlled counts in an owned preview.

Only synthetic vector() rules and a numeric-loopback mock receiver are created.
The real Platform 1.0 channel is inspected, never enabled or used. All created
fixtures are disabled on exit. Credentials and key material are never exported.
"""
import argparse
import json
from pathlib import Path
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--local-only', action='store_true')
    parser.add_argument('--max-count', type=int, choices=[0, 1], default=0)
    args = parser.parse_args()
    status = json.loads(subprocess.check_output(['python3', '-B', 'scripts/preview.py', 'status'], cwd=ROOT))
    if (status.get('role') != 'development' or not status.get('running') or status.get('sourceChanged')
            or status.get('frontendChanged') or not status.get('local', {}).get('ok')
            or (not args.local_only and not status.get('public', {'ok': True}).get('ok'))):
        raise RuntimeError('refresh and verify the owned development preview first')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    base = 'http://127.0.0.1:' + str(status['port'])
    headers = {'Content-Type': 'application/json', 'X-Language': 'en_US'}

    def api(method, path, data=None):
        request = urllib.request.Request(base + '/api/n9e' + path, method=method, headers=headers,
                                         data=json.dumps(data).encode() if data is not None else None)
        with opener.open(request, timeout=15) as response:
            result = json.load(response)
        if result.get('err') or result.get('error'):
            raise RuntimeError(path + ': ' + str(result.get('err') or result.get('error')))
        return result.get('dat', result.get('data'))

    credentials = json.loads(Path(status['credentialsFile']).read_text())
    login = api('POST', '/auth/login', credentials)
    headers['Authorization'] = 'Bearer ' + login['access_token']
    channels = api('GET', '/notify-channel-configs')
    real = [c for c in channels if c.get('request_config', {}).get('external_push_config', {}).get('mode') == 'platform10']
    if any(c['enable'] for c in real):
        raise RuntimeError('real receiver channel is enabled; do not run a simulation window')
    # A global subscription could route a synthetic rule to an unrelated real
    # provider. Do not change existing subscriptions merely to run this check.
    subscriptions = api('GET', '/busi-groups/alert-subscribes')
    if any(s.get('disabled') != 1 and (0 in (s.get('rule_ids') or []) or
           (s.get('rule_id', 0) == 0 and not s.get('rule_ids'))) for s in subscriptions):
        raise RuntimeError('an enabled global subscription exists; isolate it before running synthetic alerts')
    original = {t['id']: (t['state'], t['attempts']) for t in api('GET', '/external-push-tasks')}
    group = api('GET', '/busi-groups')[0]['id']
    team = api('GET', '/user-groups')[0]['id']
    data = api('POST', '/datasource/list', {'plugin_type': 'prometheus'})
    datasource = next(d['id'] for d in data if d['identifier'] == 'preview-shared-prometheus')
    received = []

    class Mock(BaseHTTPRequestHandler):
        def do_POST(self):
            if self.path != '/mock':
                self.send_error(404)
                return
            assert self.headers['Content-Type'] == 'application/x-www-form-urlencoded; charset=UTF-8'
            form = urllib.parse.parse_qs(self.rfile.read(int(self.headers['Content-Length'])).decode(), strict_parsing=True)
            assert set(form) == {'json'} and len(form['json']) == 1
            payload = json.loads(form['json'][0])
            assert isinstance(payload['deviceTags'], str)
            received.append(payload)
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b'{"status":200,"msg":"LOCAL MOCK ONLY"}')

        def log_message(self, *_args):
            pass

    server = ThreadingHTTPServer(('127.0.0.1', 0), Mock)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    name = 'label-simulation-' + uuid.uuid4().hex[:10]
    channel_id = None
    alert = notify = None
    evidence = {'scope': 'ISOLATED PREVIEW + LOCAL MOCK + SYNTHETIC EVENTS ONLY', 'buildId': status['buildId'],
                'local': status.get('local'), 'public': status.get('public'), 'source': status.get('source')}
    try:
        channel = {'name': name, 'ident': 'external_push', 'enable': True, 'request_type': 'external_push',
                   'param_config': {'custom': {'params': []}}, 'request_config': {'external_push_config': {
                       'mode': 'simulation', 'endpoint': f'http://127.0.0.1:{server.server_port}/mock',
                       'destination': name, 'timeout_ms': 1000, 'problem_type': 'problem', 'recovery_type': 'ok',
                       'test_sign': 'TEST_ONLY_NOT_A_PRODUCTION_SIGNATURE', 'time_zone': 'Asia/Shanghai'}}}
        channel_id = api('POST', '/notify-channel-configs', [channel])[0]
        channel['id'] = channel_id
        params = {'mapping_version': 'event_labels_v1', 'company_id': 'TEST_CUSTOMER', 'device_source': 'event',
                  'device_type_label': 'kind', 'device_name_label': 'node', 'device_tags_source': 'event_labels',
                  'device_tags_labels': json.dumps([{'label': 'model', 'required': True}, {'label': 'business', 'required': False}, {'label': 'role', 'required': False}]),
                  'level_map': '{"1":"灾难","2":"严重","3":"警告"}', 'repeat_policy': 'once_per_incident'}
        notify = api('POST', '/notify-rules', [{'name': name, 'enable': True, 'description': 'LOCAL LABEL TEST', 'user_group_ids': [team], 'pipeline_configs': [],
                                           'notify_configs': [{'channel_id': channel_id, 'template_id': 0, 'params': params, 'severities': [1, 2, 3]}]}])[0]
        alert = {'name': name, 'group_id': group, 'cate': 'prometheus', 'prod': 'metric', 'disabled': 0,
                 'datasource_queries': [{'match_type': 0, 'op': 'in', 'values': [datasource]}],
                 'rule_config': {'queries': [{'prom_ql': 'vector(1) > 0', 'severity': 2}], 'inhibit': True},
                 'prom_eval_interval': 1, 'cron_pattern': '@every 1s', 'prom_for_duration': 0, 'recover_duration': 0,
                 'notify_version': 1, 'notify_rule_ids': [notify['id']], 'notify_recovered': 1,
                 'notify_repeat_step': 1, 'notify_max_number': args.max_count,
                 'append_tags': ['ident=' + name, 'kind=服务器', 'node=TEST_192.0.2.8:9100(端口/1)', 'model=R650', 'role=数据库'],
                 'annotations': {}, 'callbacks': [], 'notify_channels': [], 'notify_groups': []}
        result = api('POST', f'/busi-group/{group}/alert-rules', [alert])
        if result and result.get(name):
            raise RuntimeError('synthetic rule rejected')
        alert = next(r for r in api('GET', f'/busi-group/{group}/alert-rules') if r['name'] == name)

        def tasks():
            return sorted(api('GET', '/external-push-tasks?channel_id=' + str(channel_id)), key=lambda r: r['id'])

        def wait(predicate, label):
            deadline = time.monotonic() + 150
            while time.monotonic() < deadline:
                rows = tasks()
                failures = [r for r in rows if r['state'] in ('validation_failed', 'unknown', 'manual')]
                if failures:
                    raise RuntimeError(label + ': unexpected task state ' + str([(r['id'], r['state'], r['reason']) for r in failures]))
                if predicate(rows):
                    print(label + ': observed', flush=True)
                    return rows
                time.sleep(1)
            raise RuntimeError('timed out: ' + label)

        rows = wait(lambda rows: any(r['state'] == 'accepted' for r in rows), 'first fault')
        first = next(r for r in rows if r['state'] == 'accepted')
        assert len(received) == 1 and received[0]['deviceTags'] == 'R650,数据库' and received[0]['level'] == '严重'
        detail = api('GET', '/external-push-tasks/' + str(first['id']))
        assert detail['omitted_tags'] == [{'label': 'business', 'reason': 'missing'}]
        count_before = len(tasks())
        preview = api('POST', '/external-push-preview', {'channel_id': channel_id, 'event_id': first['event_id'], 'params': params})
        assert preview['payload']['sign'] == '[REDACTED]' and preview['payload']['deviceTags'] == 'R650,数据库'
        assert len(received) == 1 and len(tasks()) == count_before
        if args.max_count == 1:
            deadline = time.monotonic() + 70
            while time.monotonic() < deadline:
                assert len(tasks()) == 1 and len(received) == 1, 'engine max=1 dispatched a reminder'
                time.sleep(1)
            print('engine max=1: no repeat after one-minute interval', flush=True)
            faults = 1
        else:
            rows = wait(lambda rows: len([r for r in rows if r['state'] == 'accepted']) >= 2, 'one-minute reminder delivered')
            assert len(received) == 2 and received[1]['alarmType'] == 'problem'
            faults = 2
        alert['rule_config']['queries'][0]['prom_ql'] = 'vector(0) > 0'
        api('PUT', f'/busi-group/{group}/alert-rule/{alert["id"]}', alert)
        rows = wait(lambda rows: len([r for r in rows if r['state'] == 'accepted']) == faults + 1, 'recovery')
        assert len(received) == faults + 1 and received[faults]['alarmType'] == 'ok'
        for key in ('companyId', 'deviceType', 'deviceName', 'deviceTags', 'level', 'problemTime', 'sign'):
            assert received[0][key] == received[faults][key]
        alert['rule_config']['queries'][0]['prom_ql'] = 'vector(1) > 0'
        api('PUT', f'/busi-group/{group}/alert-rule/{alert["id"]}', alert)
        rows = wait(lambda rows: len([r for r in rows if r['state'] == 'accepted']) == faults + 2, 'new incident')
        assert len(received) == faults + 2 and received[-1]['alarmType'] == 'problem'
        assert rows[-1]['incident_id'] != first['incident_id']
        now = {t['id']: (t['state'], t['attempts']) for t in api('GET', '/external-push-tasks')}
        assert all(now[k] == value for k, value in original.items())
        evidence.update({'channelId': channel_id, 'notifyRuleId': notify['id'], 'alertRuleId': alert['id'], 'name': name,
                         'tasks': rows, 'firstEventID': first['event_id'], 'received': received, 'preview': preview,
                         'oldLedgerUnchanged': True, 'requests': faults + 2, 'repeatRequestCount': faults - 1, 'maxCount': args.max_count, 'oldLedgerCount': len(original), 'basicAndPipeline': {'description': notify.get('description'), 'pipeline_configs': notify.get('pipeline_configs')}})
    finally:
        if channel_id:
            channel['enable'] = False
            api('PUT', '/notify-channel-config/' + str(channel_id), channel)
        if alert and alert.get('id'):
            alert['disabled'] = 1
            api('PUT', f'/busi-group/{group}/alert-rule/{alert["id"]}', alert)
        if notify:
            notify['enable'] = False
            api('PUT', '/notify-rule/' + str(notify['id']), notify)
        server.shutdown()
        evidence['fixturesDisabled'] = True
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')
        args.output.chmod(0o600)
    print(json.dumps({'buildId': status['buildId'], 'requests': len(received), 'channelId': channel_id, 'notifyRuleId': notify['id'] if notify else None, 'fixturesDisabled': True}))


if __name__ == '__main__':
    main()
