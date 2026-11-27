import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, DatePicker, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tabs, Tag, Upload, message } from 'antd';
import { UploadOutlined } from '@ant-design/icons';
import { Link, useLocation } from 'react-router-dom';
import moment from 'moment';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import PageLayout from '@/components/pageLayout';
import request from '@/utils/request';
import { api, blankSpec, Config, errorText, Knowledge, Spec, time, Version } from '../opsShared/api';
import '../opsShared/style.less';
import ProcedureFields from '../opsShared/ProcedureFields';
import LibraryDocuments from './LibraryDocuments';
export default function KnowledgePage() {
  const sourceVersion = Number(new URLSearchParams(useLocation().search).get('version')) || 0;
  return (
    <PageLayout title='运维知识'>
      <Tabs key={sourceVersion} defaultActiveKey={sourceVersion ? 'procedures' : 'documents'}>
        <Tabs.TabPane tab='资料知识' key='documents'>
          <LibraryDocuments />
        </Tabs.TabPane>
        <Tabs.TabPane tab='处置规程' key='procedures'>
          <OpsKnowledge />
        </Tabs.TabPane>
      </Tabs>
    </PageLayout>
  );
}

type Source = { id: number; name: string; media_type: string; size: number; sha256: string; description: string };
function OpsKnowledge() {
  const [config, setConfig] = useState<Config>();
  const [group, setGroup] = useState<number>();
  const [items, setItems] = useState<Knowledge[]>([]);
  const [sources, setSources] = useState<Source[]>([]);
  const [editing, setEditing] = useState<Knowledge | null>();
  const [versions, setVersions] = useState<Version[]>();
  const [reading, setReading] = useState<Version>();
  const sourceVersion = Number(new URLSearchParams(useLocation().search).get('version')) || 0;
  useEffect(() => {
    let active = true;
    if (sourceVersion)
      api<{ version: Version }>(`/versions/${sourceVersion}`)
        .then((result) => {
          if (active) setReading(result.version);
        })
        .catch((e) => {
          if (active) setError(errorText(e));
        });
    return () => {
      active = false;
    };
  }, [sourceVersion]);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [sourceDialog, setSourceDialog] = useState<string>();
  const [sourceForm] = Form.useForm();
  const [form] = Form.useForm();
  const epoch = useRef(0);
  const load = useCallback(async () => {
    const seq = ++epoch.current;
    try {
      const cfg = await api<Config>('/config');
      if (seq !== epoch.current) return;
      setConfig(cfg);
      const selected = group || cfg.groups[0]?.id;
      if (!group && selected) {
        setGroup(selected);
        return;
      }
      const data = selected ? await api<Knowledge[]>(`/knowledge?group_id=${selected}`) : [];
      const docs = selected ? await api<Source[]>(`/sources?group_id=${selected}`) : [];
      if (seq === epoch.current) {
        setItems(data);
        setSources(docs);
        setError('');
      }
    } catch (e) {
      if (seq === epoch.current) setError(errorText(e));
    }
  }, [group]);
  useEffect(() => {
    load();
    return () => {
      epoch.current++;
    };
  }, [load]);
  const writable = config?.groups.find((g) => g.id === group)?.writable;
  const edit = (item?: Knowledge) => {
    const spec = item?.draft || blankSpec();
    form.resetFields();
    form.setFieldsValue({
      title: item?.title || '',
      ...spec,
      conditions_rows: Object.entries(spec.conditions || {}).map(([name, value]) => ({ name, value })),
      rule_ids_text: (spec.rule_ids || []).join(', '),
      validated_date: spec.validated_at ? moment.unix(spec.validated_at) : undefined,
      review_date: spec.review_at ? moment.unix(spec.review_at) : undefined,
    });
    setEditing(item || null);
  };
  const save = async () => {
    const value = await form.validateFields();
    setSaving(true);
    try {
      const spec: Spec = {
        summary: value.summary || '',
        body: value.body || '',
        source_ids: value.source_ids || [],
        source_note: value.source_note || '',
        rule_ids: String(value.rule_ids_text || '')
          .split(/[\s,]+/)
          .filter(Boolean)
          .map(Number),
        conditions: Object.fromEntries((value.conditions_rows || []).map((c: { name: string; value: string }) => [c.name, c.value])),
        steps: value.steps || [],
        actions: value.actions || [],
        checks: value.checks || [],
        escalation: value.escalation || '',
        validated_at: value.validated_date?.unix() || 0,
        review_at: value.review_date?.unix() || 0,
        test_only: !!value.test_only,
      };
      await api(editing ? `/knowledge/${editing.id}` : '/knowledge', editing ? 'PUT' : 'POST', { group_id: group, revision: editing?.revision, title: value.title, spec });
      setEditing(undefined);
      await load();
      message.success('草稿已保存，审核发布后可用于处置');
    } catch (e) {
      message.error(errorText(e));
    } finally {
      setSaving(false);
    }
  };
  const publish = (item: Knowledge) =>
    Modal.confirm({
      title: '确认审核并发布此知识？',
      content: '请确认资料来源、适用环境、排查步骤、操作风险和恢复标准已经验证。发布后形成不可变版本。',
      onOk: async () => {
        try {
          await api(`/knowledge/${item.id}/publish`, 'POST', { revision: item.revision });
          await load();
        } catch (e) {
          message.error(errorText(e));
          throw e;
        }
      },
    });
  const history = async (item: Knowledge) => {
    try {
      setVersions(await api<Version[]>(`/knowledge/${item.id}/versions`));
    } catch (e) {
      message.error(errorText(e));
    }
  };
  const withdraw = (v: Version) => {
    let reason = '';
    Modal.confirm({
      title: '停用知识版本',
      content: (
        <Input.TextArea
          placeholder='说明失效或风险原因'
          onChange={(e) => {
            reason = e.target.value;
          }}
        />
      ),
      onOk: async () => {
        if (!reason.trim()) throw new Error('请填写停用原因');
        try {
          await api(`/versions/${v.id}/withdraw`, 'POST', { reason });
          setVersions(undefined);
          setReading(undefined);
          await load();
        } catch (e) {
          message.error(errorText(e));
          throw e;
        }
      },
    });
  };
  const download = async (source: Source) => {
    try {
      const blob = await request(`/api/n9e/ops/sources/${source.id}/file`, { responseType: 'blob', silence: true });
      const u = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = u;
      a.download = source.name;
      a.click();
      setTimeout(() => URL.revokeObjectURL(u), 1000);
    } catch (e) {
      message.error(errorText(e));
    }
  };
  return (
    <div>
      <div className='ops-page'>
        {error && <Alert type='error' message={error} showIcon style={{ marginBottom: 16 }} />}
        <div className='ops-toolbar'>
          <Link to='/ops/cases'>进入告警处置</Link>
          <Select style={{ minWidth: 240 }} placeholder='选择业务组' value={group} onChange={setGroup} options={config?.groups.map((g) => ({ value: g.id, label: g.name }))} />
          <Button onClick={load}>刷新</Button>
          <Button type='primary' disabled={!writable} onClick={() => edit()}>
            编写知识
          </Button>
          <Upload
            showUploadList={false}
            accept='.md,.txt,.pdf,.docx'
            disabled={!writable}
            beforeUpload={(file) => {
              const data = new FormData();
              data.append('file', file);
              data.append('group_id', String(group));
              request('/api/n9e/ops/sources', { method: 'POST', data, requestType: 'form', silence: true })
                .then((res) => {
                  if (!res || res.err !== '') throw new Error(res?.err || '上传失败');
                  message.success('来源资料已保存');
                  load();
                })
                .catch((e) => message.error(errorText(e)));
              return false;
            }}
          >
            <Button disabled={!writable} icon={<UploadOutlined />}>
              录入来源资料
            </Button>
          </Upload>
          <Button
            disabled={!writable}
            onClick={() => {
              sourceForm.resetFields();
              setSourceDialog('archive');
            }}
          >
            引用档案版本
          </Button>
          <Button
            disabled={!writable || !config?.model_enabled}
            onClick={() => {
              sourceForm.resetFields();
              setSourceDialog('generate');
            }}
          >
            从资料生成草稿
          </Button>
        </div>
        <Modal
          title={sourceDialog === 'archive' ? '引用确切档案文档版本' : '从来源资料生成未验证草稿'}
          visible={!!sourceDialog}
          confirmLoading={saving}
          onCancel={() => setSourceDialog(undefined)}
          onOk={async () => {
            const value = await sourceForm.validateFields();
            setSaving(true);
            try {
              const result = await api<Knowledge>(sourceDialog === 'archive' ? '/sources/from-archive' : '/knowledge-draft', 'POST', { ...value, group_id: group });
              setSourceDialog(undefined);
              await load();
              if (sourceDialog === 'generate') edit(result);
              else message.success('已将该版本原件保存为本组私有来源');
            } catch (e) {
              message.error(errorText(e));
            } finally {
              setSaving(false);
            }
          }}
        >
          <Form form={sourceForm} layout='vertical'>
            {sourceDialog === 'archive' ? (
              <>
                <Form.Item name='version_id' label='档案中的文档已保存版本 ID' rules={[{ required: true }]}>
                  <InputNumber min={1} />
                </Form.Item>
                <Alert type='info' message='保存确切版本的私有副本并记录来源编号。后续档案编辑不会改变该来源；请核实资料可供本业务组使用。' />
              </>
            ) : (
              <>
                <Form.Item name='source_ids' label='选择已脱敏的 TXT / Markdown 来源（最多 5 份，合计 64 KB）' rules={[{ required: true }]}>
                  <Select mode='multiple' options={sources.filter((s) => s.media_type.startsWith('text/plain')).map((s) => ({ value: s.id, label: s.name }))} />
                </Form.Item>
                <Alert type='info' message='使用管理员已启用的模型连接处理所选文本；生成结果保留为未验证草稿。PDF / DOCX 请先整理并核对文字摘录，发布仍需人员审核。' />
              </>
            )}
          </Form>
        </Modal>
        <Tabs defaultActiveKey='knowledge'>
          <Tabs.TabPane tab='知识与版本' key='knowledge'>
            <Table
              rowKey='id'
              dataSource={items}
              columns={[
                { title: '知识标题', dataIndex: 'title' },
                { title: '状态', render: (_, row) => (row.current_version_id ? <Tag color='green'>已有正式版本</Tag> : <Tag>草稿</Tag>) },
                { title: '更新时间', dataIndex: 'updated_at', render: time },
                {
                  title: '操作',
                  render: (_, row) => (
                    <Space>
                      {writable && (
                        <>
                          <Button size='small' onClick={() => edit(row)}>
                            编辑草稿
                          </Button>
                          <Button size='small' onClick={() => publish(row)}>
                            审核发布
                          </Button>
                        </>
                      )}
                      <Button size='small' disabled={!row.current_version_id} onClick={() => history(row)}>
                        查看正式版本
                      </Button>
                    </Space>
                  ),
                },
              ]}
            />
          </Tabs.TabPane>
          <Tabs.TabPane tab={`来源资料 (${sources.length})`} key='sources'>
            <Alert type='info' message='上传资料保留原件；在编写知识时引用并整理适用条件和步骤。扫描件或复杂附件需要人员确认内容。' style={{ marginBottom: 12 }} />
            <Table
              rowKey='id'
              dataSource={sources}
              columns={[
                {
                  title: '资料名称',
                  dataIndex: 'name',
                  render: (name, row) => (
                    <Button type='link' onClick={() => download(row)}>
                      {name}
                    </Button>
                  ),
                },
                { title: '大小', dataIndex: 'size', render: (v) => `${v.toLocaleString()} 字节` },
                { title: '说明', dataIndex: 'description' },
                { title: '内容摘要 SHA-256', dataIndex: 'sha256', ellipsis: true },
              ]}
            />
          </Tabs.TabPane>
        </Tabs>
        <Modal
          visible={editing !== undefined}
          width={1000}
          title={editing ? '编辑知识草稿' : '编写知识草稿'}
          onCancel={() => setEditing(undefined)}
          onOk={save}
          confirmLoading={saving}
          destroyOnClose
          bodyStyle={{ maxHeight: '75vh', overflowY: 'auto' }}
        >
          <Form form={form} layout='vertical' className='ops-editor'>
            <Form.Item name='title' label='知识标题' rules={[{ required: true }]}>
              <Input maxLength={80} />
            </Form.Item>
            <Form.Item name='summary' label='适用问题和处理目标' rules={[{ required: true }]}>
              <Input.TextArea rows={2} />
            </Form.Item>
            <Form.Item name='body' label='说明正文（Markdown）'>
              <Input.TextArea rows={5} />
            </Form.Item>
            <Form.Item name='source_ids' label='引用来源资料'>
              <Select mode='multiple' options={sources.map((s) => ({ value: s.id, label: s.name }))} />
            </Form.Item>
            <Form.Item name='source_note' label='来源及验证依据（可记录工程师访谈或演练）'>
              <Input.TextArea rows={2} />
            </Form.Item>
            <Space align='start'>
              <Form.Item name='validated_date' label='最近验证时间' rules={[{ required: true }]}>
                <DatePicker showTime />
              </Form.Item>
              <Form.Item name='review_date' label='下次复核期限' rules={[{ required: true }]}>
                <DatePicker showTime />
              </Form.Item>
              <Form.Item name='test_only' label='仅测试环境' valuePropName='checked'>
                <Switch />
              </Form.Item>
            </Space>
            <Form.Item name='rule_ids_text' label='关联告警规则 ID（逗号分隔，可选）'>
              <Input />
            </Form.Item>
            <ProcedureFields form={form} demoEnabled={config?.demo_enabled} />
            <Form.Item name='escalation' label='升级条件与联系路径'>
              <Input.TextArea rows={3} />
            </Form.Item>
          </Form>
        </Modal>
        <Modal visible={!!versions} title='正式版本历史' width={900} footer={null} onCancel={() => setVersions(undefined)}>
          <Table
            rowKey='id'
            dataSource={versions}
            pagination={false}
            columns={[
              { title: '版本', dataIndex: 'sequence', render: (v) => `v${v}` },
              { title: '发布时间', dataIndex: 'published_at', render: time },
              { title: '状态', render: (_, v) => (v.withdrawn_at ? <Tag color='red'>已停用</Tag> : <Tag color='green'>正式版本</Tag>) },
              {
                title: '操作',
                render: (_, v) => (
                  <Space>
                    <Button onClick={() => setReading(v)}>阅读</Button>
                    {writable && !v.withdrawn_at && (
                      <Button danger onClick={() => withdraw(v)}>
                        停用
                      </Button>
                    )}
                  </Space>
                ),
              },
            ]}
          />
        </Modal>
        <Modal visible={!!reading} title={reading && `${reading.title} · v${reading.sequence}`} width={850} footer={null} onCancel={() => setReading(undefined)}>
          <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml>
            {reading?.payload.body || reading?.payload.summary || ''}
          </ReactMarkdown>
          <h4>排查步骤</h4>
          {reading?.payload.steps.map((s) => (
            <p key={s.key}>
              <strong>{s.title}</strong>：{s.instruction}
            </p>
          ))}
          <h4>恢复标准</h4>
          {reading?.payload.checks.map((c) => (
            <p key={c.key}>
              {c.title}
              {c.required && '（必需）'} · {c.kind === 'manual' ? '人员确认' : `${c.query} ${c.operator} ${c.threshold}`}
            </p>
          ))}
        </Modal>
      </div>
    </div>
  );
}
