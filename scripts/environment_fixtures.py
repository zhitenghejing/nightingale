#!/usr/bin/env python3
"""Seed synthetic targets only into an explicitly selected disposable preview."""
import argparse
import json
from pathlib import Path
import subprocess
import uuid
import time


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--instance', required=True, type=Path)
    p.add_argument('--output', required=True, type=Path)
    args = p.parse_args()
    manifest = json.loads((args.instance / 'manifest.json').read_text())
    if manifest['role'] != 'development' or not manifest['storage']['databaseName'].startswith('n9ep_'):
        raise SystemExit('Refusing non-disposable storage')
    state = args.instance.parent.parent
    services = json.loads((state / 'shared/services.json').read_text())
    database = manifest['storage']['databaseName']

    def query(sql):
        r = subprocess.run(['docker', 'exec', '-i', services['postgres'], 'psql', '-X', '-U', 'postgres', '-d', database, '-v', 'ON_ERROR_STOP=1', '-At'], input=sql, capture_output=True, text=True)
        if r.returncode:
            raise RuntimeError('Fixture SQL failed; private database retained for diagnosis')
        return r.stdout.strip()

    def literal(value):
        return "'" + str(value).replace("'", "''") + "'"

    group = int(query("SELECT id FROM busi_group ORDER BY id LIMIT 1"))
    suffix = uuid.uuid4().hex[:8]
    names = ['sim-env-app-a-' + suffix, 'sim-env-app-b-' + suffix, 'sim-env-f5-a-' + suffix, 'sim-env-f5-b-' + suffix]
    targets = []
    for i, name in enumerate(names):
        ident = literal(name)
        tid = int(query(f"INSERT INTO target(group_id,ident,note,tags,host_ip,os,update_at) VALUES({group},{ident},'隔离模拟设备，无真实采样','environment=simulation','10.250.19.{11+i}','simulation',0) RETURNING id" ).splitlines()[0])
        query(f"INSERT INTO target_busi_group(target_ident,group_id,update_at) VALUES({ident},{group},0)")
        targets.append({'id': tid, 'ref': f'target:{tid}', 'ident': name})
    # A persisted historical test event is inserted without invoking dispatch.
    # Use the table's own required-column metadata to preserve native schema compatibility.
    columns = json.loads(query("SELECT coalesce(json_agg(json_build_object('name',column_name,'type',data_type)),'[]') FROM information_schema.columns WHERE table_name='alert_his_event' AND is_nullable='NO' AND column_default IS NULL"))
    values = {row['name']: ('false' if row['type'] == 'boolean' else "'{}'" if row['type'] in ('json', 'jsonb') else '0' if row['type'] in ('smallint', 'integer', 'bigint', 'numeric', 'real', 'double precision') else "''") for row in columns}
    values.update(group_id=str(group), rule_id='0', rule_name=literal('Sim isolation event ' + suffix), target_ident=literal(names[0]), trigger_time=str(int(time.time())), first_trigger_time=str(int(time.time())), dispatch_id=literal(uuid.uuid4()), incident_id=literal(uuid.uuid4()), incident_sequence='1', hash=literal(uuid.uuid4().hex), cate=literal('prometheus'))
    statement = 'INSERT INTO alert_his_event (' + ','.join('"' + key + '"' for key in values) + ') VALUES (' + ','.join(values.values()) + ') RETURNING id'
    alert_id = int(query(statement).splitlines()[0])
    # Inventory uses its native endpoint later; fixtures do not touch shared metrics.
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps({'group_id': group, 'suffix': suffix, 'targets': targets, 'database_is_disposable': True, 'alert_ref': f'alert:{alert_id}'}, ensure_ascii=False, indent=2))
    args.output.chmod(0o600)
    print('Synthetic targets created in selected disposable preview; IDs saved privately')


if __name__ == '__main__':
    main()
