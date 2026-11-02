#!/usr/bin/env python3
"""Exercise only this worktree's disposable preview and a loopback mock receiver.

Creates isolated simulation rules through existing UI APIs, observes real alert
engine fire/recover/new-incident dispatches, and disables the created fixtures on
exit. Never enables service APIs or connects to the production receiver.
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
    parser.add_argument('--output', type=Path, required=True, help='Private local evidence JSON path')
    parser.add_argument('--local-only', action='store_true', help='Local diagnostic only; records public probe failure and does not qualify a public handoff')
    parser.add_argument('--regressions', action='store_true', help='Also exercise real notify-only mute, filtered reminder and mid-incident channel enable (several minutes)')
    args = parser.parse_args()
    status = json.loads(subprocess.check_output(['python3', '-B', 'scripts/preview.py', 'status'], cwd=ROOT))
    if (status.get('role') != 'development' or not status.get('running') or status.get('sourceChanged')
            or status.get('frontendChanged') or not status.get('local', {}).get('ok')
            or (not args.local_only and not status.get('public', {'ok': True}).get('ok'))):
        raise RuntimeError('refresh and verify this disposable development preview first')
    base = 'http://127.0.0.1:' + str(status['port'])
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    headers = {'Content-Type': 'application/json', 'X-Language': 'en_US'}

    def api(method, path, data=None):
        request = urllib.request.Request(base + '/api/n9e' + path, method=method, headers=headers,
                                         data=json.dumps(data).encode() if data is not None else None)
        with opener.open(request, timeout=15) as response:
            body = json.load(response)
        if body.get('err') or body.get('error'):
            raise RuntimeError(path + ': ' + str(body.get('err') or body.get('error')))
        return body['dat'] if 'dat' in body else body.get('data')

    credentials = json.loads(Path(status['credentialsFile']).read_text())
    login = api('POST', '/auth/login', credentials)
    headers['Authorization'] = 'Bearer ' + login['access_token']
    groups = api('GET', '/busi-groups')
    group = groups[0]['id']
    datasources = api('POST', '/datasource/list', {'plugin_type': 'prometheus'})
    datasource = next(item['id'] for item in datasources if item['identifier'] == 'preview-shared-prometheus')
    received = []

    class Mock(BaseHTTPRequestHandler):
        def do_POST(self):
            if self.path != '/mock':
                self.send_error(404)
                return
            assert self.headers['Content-Type'] == 'application/x-www-form-urlencoded; charset=UTF-8'
            form = urllib.parse.parse_qs(self.rfile.read(int(self.headers['Content-Length'])).decode(), strict_parsing=True)
            assert set(form) == {'json'} and len(form['json']) == 1
            value = json.loads(form['json'][0])
            assert isinstance(value['deviceTags'], str)
            received.append(value)
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(b'{"status":200,"msg":"mock accepted"}')

        def log_message(self, *_args):
            pass

    server = ThreadingHTTPServer(('127.0.0.1', 0), Mock)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    name = 'external-simulation-' + uuid.uuid4().hex[:10]
    channel = {
        'name': name, 'ident': 'external_push', 'enable': True, 'request_type': 'external_push',
        'param_config': {'custom': {'params': []}},
        'request_config': {'external_push_config': {
            'mode': 'simulation', 'endpoint': 'http://127.0.0.1:' + str(server.server_port) + '/mock',
            'destination': name, 'timeout_ms': 1000, 'problem_type': 'problem', 'recovery_type': 'ok',
            'test_sign': 'TEST_ONLY_NOT_PRODUCTION', 'time_zone': 'Asia/Shanghai',
        }},
    }
    alert = None
    notify = None
    channel_id = None
    mute_id = None
    evidence = {'buildId': status['buildId'], 'source': status.get('source', {}), 'simulation': True,
                'local_only': args.local_only, 'local_probe': status.get('local'), 'public_probe': status.get('public')}
    try:
        channel_id = api('POST', '/notify-channel-configs', [channel])[0]
        channel['id'] = channel_id
        params = {'company_id': '模拟客户 + &=%', 'device_type': 'TEST_virtualization', 'device_name_label': 'ident',
                  'device_tags': "['中文+ &=%', 'TEST_ONLY']", 'level_map': '{"1":"TEST_严重"}', 'repeat_policy': 'all_dispatches'}
        notify = api('POST', '/notify-rules', [{'name': name, 'enable': True, 'user_group_ids': [],
                     'notify_configs': [{'channel_id': channel_id, 'template_id': 0, 'params': params,
                                         'severities': [1, 2, 3], 'time_ranges': [], 'label_keys': [], 'attributes': []}]}])[0]
        # This expression reads the shared test Prometheus only. No collector,
        # remote-write path, service API or production database is involved.
        alert = {'name': name, 'group_id': group, 'cate': 'prometheus', 'prod': 'metric', 'disabled': 0,
                 'datasource_queries': [{'match_type': 0, 'op': 'in', 'values': [datasource]}],
                 'rule_config': {'queries': [{'prom_ql': 'vector(1) > 0', 'severity': 1}], 'inhibit': True},
                 'prom_eval_interval': 1, 'cron_pattern': '@every 1s', 'prom_for_duration': 0, 'recover_duration': 0,
                 'notify_version': 1, 'notify_rule_ids': [notify['id']], 'notify_recovered': 1,
                 'notify_repeat_step': 0, 'notify_max_number': 0, 'append_tags': ['ident=' + name],
                 'annotations': {}, 'callbacks': [], 'notify_channels': [], 'notify_groups': []}
        result = api('POST', f'/busi-group/{group}/alert-rules', [alert])
        if result.get(name):
            raise RuntimeError('synthetic rule rejected: ' + result[name])
        rules = api('GET', f'/busi-group/{group}/alert-rules')
        alert = next(row for row in rules if row['name'] == name)

        def tasks():
            return sorted(api('GET', '/external-push-tasks?channel_id=' + str(channel_id)), key=lambda row: row['id'])

        def wait_for(count):
            deadline = time.monotonic() + 150
            while time.monotonic() < deadline:
                rows = tasks()
                failures = [row for row in rows if row['state'] not in ('pending', 'sending', 'accepted', 'waiting_predecessor')]
                if failures:
                    raise RuntimeError('simulation task failed: ' + str([(r['state'], r['reason']) for r in failures]))
                accepted = [row for row in rows if row['state'] == 'accepted']
                if len(accepted) >= count:
                    return accepted
                time.sleep(1)
            raise RuntimeError('timed out observing synthetic rule; inspect private preview logs')

        first = wait_for(1)
        assert len(received) == 1 and received[0]['alarmType'] == 'problem'
        alert['rule_config']['queries'][0]['prom_ql'] = 'vector(0) > 0'
        api('PUT', f'/busi-group/{group}/alert-rule/{alert["id"]}', alert)
        second = wait_for(2)
        assert received[1]['alarmType'] == 'ok'
        assert first[0]['incident_id'] == second[1]['incident_id']
        assert received[0]['problemTime'] == received[1]['problemTime']
        alert['rule_config']['queries'][0]['prom_ql'] = 'vector(1) > 0'
        api('PUT', f'/busi-group/{group}/alert-rule/{alert["id"]}', alert)
        final = wait_for(3)
        assert final[2]['incident_id'] != final[0]['incident_id']
        assert len(received) == 3
        if args.regressions:
            print('Starting real-engine sequence regressions', flush=True)
            def update_alert(firing, repeat=1):
                alert['rule_config']['queries'][0]['prom_ql'] = 'vector(1) > 0' if firing else 'vector(0) > 0'
                alert['notify_repeat_step'] = repeat
                api('PUT', f'/busi-group/{group}/alert-rule/{alert["id"]}', alert)

            def history():
                return api('GET', f'/alert-his-events/list?rid={alert["id"]}&hours=24&limit=100')['list']

            def wait_snapshot(predicate):
                deadline = time.monotonic() + 150
                while time.monotonic() < deadline:
                    rows = [row for row in history() if predicate(row)]
                    if rows:
                        return max(rows, key=lambda row: row['id'])
                    time.sleep(1)
                raise RuntimeError('synthetic snapshot condition not observed')

            def checkpoint(label, start, expected_sequences):
                rows = wait_for(start + len(expected_sequences))[start:]
                assert [row['sequence'] for row in rows] == expected_sequences, (label, rows)
                assert [row['kind'] for row in rows] == ['problem', 'problem', 'recovery']
                assert len({row['incident_id'] for row in rows}) == 1
                return {'scenario': label, 'tasks': [{key: row[key] for key in ('id', 'sequence', 'state', 'attempts', 'incident_id')} for row in rows]}

            update_alert(False)
            wait_for(4)
            mute_id = api('POST', f'/busi-group/{group}/alert-mutes', {
                'prod': 'metric', 'cate': 'prometheus', 'datasource_ids': [datasource],
                'tags': [{'key': 'ident', 'func': '==', 'value': name}], 'cause': 'TEST regression only',
                'btime': int(time.time()) - 30, 'etime': int(time.time()) + 900,
                'disabled': 0, 'mute_type': 1, 'mute_time_type': 0, 'severities': [1],
            })
            time.sleep(11)  # Existing caches poll every nine seconds.
            history_before = max(row['id'] for row in history())
            update_alert(True)
            muted = wait_snapshot(lambda row: row['id'] > history_before and not row['is_recovered'])
            assert muted['incident_sequence'] == 1 and len(tasks()) == 4
            api('DELETE', f'/busi-group/{group}/alert-mutes', {'ids': [mute_id]})
            mute_id = None
            wait_for(5)  # Unmute sends the first real problem with source sequence 2.
            wait_for(6)  # A normal one-minute reminder remains a distinct dispatch.
            update_alert(False)
            scenarios = [checkpoint('first problem notify-muted then unmuted', 4, [2, 3, 4])]
            print('PASS: notify-only mute/unmute with reminder and recovery', flush=True)

            # The first actual task is accepted, then one real engine reminder
            # is filtered by the existing severity switch, without creating a task.
            update_alert(True)
            first_filtered = wait_for(8)[7]
            notify['notify_configs'][0]['severities'] = [2]
            api('PUT', '/notify-rule/' + str(notify['id']), notify)
            excluded = wait_snapshot(lambda row: row['incident_id'] == first_filtered['incident_id'] and row['incident_sequence'] == 2)
            time.sleep(2)
            assert len(tasks()) == 8
            notify['notify_configs'][0]['severities'] = [1, 2, 3]
            api('PUT', '/notify-rule/' + str(notify['id']), notify)
            wait_for(9)
            update_alert(False)
            scenarios.append(checkpoint('middle reminder severity-filtered', 7, [1, 3, 4]))
            assert excluded['dispatch_id'] not in {row['dispatch_id'] for row in tasks()}
            print('PASS: filtered middle reminder with later reminder and recovery', flush=True)

            channel['enable'] = False
            api('PUT', '/notify-channel-config/' + str(channel_id), channel)
            time.sleep(11)
            history_before = max(row['id'] for row in history())
            update_alert(True)
            disabled_first = wait_snapshot(lambda row: row['id'] > history_before and not row['is_recovered'])
            assert disabled_first['incident_sequence'] == 1 and len(tasks()) == 10
            channel['enable'] = True
            api('PUT', '/notify-channel-config/' + str(channel_id), channel)
            wait_for(11)
            wait_for(12)
            update_alert(False)
            scenarios.append(checkpoint('channel enabled mid-incident', 10, [2, 3, 4]))
            assert disabled_first['dispatch_id'] not in {row['dispatch_id'] for row in tasks()}
            print('PASS: channel enabled mid-incident with reminder and recovery', flush=True)
            final = wait_for(13)
            assert len(received) == 13
            evidence['regressions'] = scenarios
        evidence.update(channel_id=channel_id, notify_rule_id=notify['id'], alert_rule_id=alert['id'],
                        tasks=[{k: row[k] for k in ('id', 'state', 'dispatch_id', 'incident_id', 'sequence', 'attempts')} for row in final],
                        receiver_count=len(received), outcome='local simulation interface acceptance only; no personnel notification')
    finally:
        cleanup_errors = []
        if mute_id is not None:
            try:
                api('DELETE', f'/busi-group/{group}/alert-mutes', {'ids': [mute_id]})
            except Exception as error:
                cleanup_errors.append(str(error))
        for fixture, field, value, path in [
            (alert, 'disabled', 1, f'/busi-group/{group}/alert-rule/'),
            (channel if channel_id else None, 'enable', False, '/notify-channel-config/'),
            (notify, 'enable', False, '/notify-rule/'),
        ]:
            if fixture and fixture.get('id'):
                fixture[field] = value
                try:
                    api('PUT', path + str(fixture['id']), fixture)
                except Exception as error:
                    cleanup_errors.append(str(error))
        server.shutdown()
        server.server_close()
        if cleanup_errors:
            raise RuntimeError('simulation fixture cleanup failed: ' + '; '.join(cleanup_errors))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')
    args.output.chmod(0o600)
    print(json.dumps({k: evidence[k] for k in ('buildId', 'receiver_count', 'outcome')}, ensure_ascii=False))


if __name__ == '__main__':
    main()
