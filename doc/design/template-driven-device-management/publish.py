#!/usr/bin/env python3
"""Publish the standalone design alongside, and without replacing, the active UI.

Run: python3 -B doc/design/template-driven-device-management/publish.py
Only the current development preview is refreshed. No device/API writes occur.
"""
import hashlib
import html
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

from markdown_it import MarkdownIt

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
OLD_FILES = (
    'device-onboarding.html',
    'device-onboarding-plan.md',
    'device-onboarding-catalog.txt',
)


def hashes(directory):
    return {
        p.relative_to(directory).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in directory.rglob('*') if p.is_file()
    }


def retired_page():
    return '''<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>旧设备添加原型已归档</title>
<style>body{font:16px/1.8 system-ui,sans-serif;max-width:780px;margin:70px auto;padding:24px;color:#263349}a{color:#6940e8}</style>
<h1>旧设备添加原型已归档</h1>
<p>当前方案已改为：模板中心管理分类与完整模板包，新增设备按模板生成表单、采集、仪表盘和告警通知配置。</p>
<p><a href="template-center.html">打开新版模板中心与设备添加原型</a></p>
<p><a href="design.html">阅读新版设计文档</a> · <a href="archive/2026-10-28/device-onboarding.html">查看历史原型（已退役）</a></p>
<p>此处只替换独立设计原型。现有业务系统和架构图保留。</p></html>'''


def document_page(source, title):
    # Repository references remain useful in the .md source. The web copy only
    # links to routes that this publisher actually provides.
    links = {
        'README.md': 'design.html', 'prototype.html': 'template-center.html',
        'catalog-migration.md': 'catalog-migration.html',
        '../device-onboarding-prototype/archive/2026-10-28/catalog.json':
            'archive/2026-10-28/device-onboarding-catalog.txt',
    }

    def replace(match):
        label, target = match.groups()
        if target in links:
            return '[' + label + '](' + links[target] + ')'
        if target.startswith(('https://', 'http://', '#')):
            return match.group(0)
        return label + '（仓库文件：`' + target + '`）'

    content = re.sub(r'\[([^\]]+)\]\(([^)]+)\)', replace, source)
    body = MarkdownIt('commonmark', {'html': False}).enable('table').render(content)
    return '''<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>''' + html.escape(title) + '''</title>
<style>body{font:15px/1.85 system-ui,"Noto Sans CJK SC",sans-serif;color:#253349;max-width:1120px;margin:32px auto;padding:0 24px 70px}h1,h2,h3{line-height:1.4;color:#17243d}h2{margin-top:38px;border-bottom:1px solid #e3e7ed;padding-bottom:10px}a{color:#6940e8}table{border-collapse:collapse;width:100%;font-size:14px;display:block;overflow:auto}th,td{border:1px solid #dfe4ec;padding:9px 12px;text-align:left;min-width:95px}th{background:#f5f6fa}pre{background:#f3f5f9;border:1px solid #e1e5ed;border-radius:8px;padding:18px;overflow:auto}code{font-size:13px;overflow-wrap:anywhere}nav{display:flex;gap:22px;flex-wrap:wrap;border-bottom:1px solid #e3e7ed;padding-bottom:16px}blockquote{border-left:3px solid #7652ef;padding-left:18px;color:#617086}</style>
<nav><a href="template-center.html">模板中心原型</a><a href="template-center.html?view=add">新增设备原型</a><a href="design.html">设计文档</a><a href="catalog-migration.html">全部组件迁移计划</a><a href="/architecture/architecture.html">现有架构图</a></nav><main>''' + body + '</main></html>'


def main():
    script = (HERE / 'prototype.html').read_text().split('<script>', 1)[1].split('</script>', 1)[0]
    subprocess.run(['node', '--check'], input=script, text=True, check=True)
    result = subprocess.run(
        [sys.executable, '-B', 'scripts/preview.py', 'status', '--local-only'],
        cwd=ROOT, check=True, capture_output=True, text=True,
    )
    current = json.loads(result.stdout)
    if current.get('role') != 'development' or not current.get('running'):
        raise SystemExit('A running development preview is required.')
    instance = Path(current['instanceDir']).resolve()
    published = instance / 'run/pub'
    if not (published / 'index.html').is_file():
        raise SystemExit('The active preview has no file UI to preserve.')
    architecture = hashes(published / 'architecture')
    if not architecture:
        raise SystemExit('The existing architecture publication must be present.')
    original_business = {
        k: v for k, v in hashes(published).items()
        if not k.startswith(('prototype/', 'architecture/'))
    }
    stage = Path(tempfile.mkdtemp(prefix='template-catalog-assets-', dir=instance))
    shutil.copytree(published, stage, dirs_exist_ok=True)
    destination = stage / 'prototype'
    destination.mkdir(exist_ok=True)
    archive = destination / 'archive/2026-10-28'
    archive.mkdir(parents=True, exist_ok=True)
    for name in OLD_FILES:
        if not (archive / name).exists():
            original = published / 'prototype' / name
            if not original.is_file():
                raise SystemExit('Cannot archive missing previous prototype: ' + name)
            shutil.copyfile(original, archive / name)
    sources = [HERE / name for name in (
        'README.md', 'prototype.html', 'catalog-migration.md', 'publish.py',
    )]
    revision = hashlib.sha256(b''.join(p.read_bytes() for p in sources)).hexdigest()
    page = sources[1].read_text().replace(
        '<head>', '<head><meta name="template-catalog-revision" content="' + revision + '">', 1,
    )
    (destination / 'template-center.html').write_text(page)
    for source_name, served_name, title in (
        ('README.md', 'design', '分类模板与设备管理 · 设计方案'),
        ('catalog-migration.md', 'catalog-migration', '现有组件分类与迁移计划'),
    ):
        source = (HERE / source_name).read_text()
        (destination / (served_name + '.md')).write_text(source)
        (destination / (served_name + '.html')).write_text(document_page(source, title))
    (destination / 'device-onboarding.html').write_text(retired_page())
    (destination / 'device-onboarding-plan.md').write_text(
        '# 旧方案已归档\n\n请阅读 [新版设计](design.html)。'
        '历史文件在 archive/2026-10-28/ 下。\n',
    )
    (destination / 'device-onboarding-catalog.txt').write_text(json.dumps({
        'status': 'retired', 'current': 'catalog-migration.html',
        'archive': 'archive/2026-10-28/device-onboarding-catalog.txt',
    }, ensure_ascii=False, indent=2))
    if hashes(stage / 'architecture') != architecture:
        raise SystemExit('Architecture files changed unexpectedly.')
    staged_business = {
        k: v for k, v in hashes(stage).items()
        if not k.startswith(('prototype/', 'architecture/'))
    }
    if staged_business != original_business:
        raise SystemExit('Business frontend assets changed unexpectedly.')
    subprocess.run([
        sys.executable, '-B', 'scripts/preview.py', 'refresh', '--lan-host',
        '192.168.0.152', '--local-only', '--frontend-dir', str(stage),
        '--frontend-ref', 'template-catalog-v2:' + revision,
    ], cwd=ROOT, check=True)
    print('\nPrototype: http://192.168.0.152:' + str(current['port']) + '/prototype/template-center.html')
    print('Prototype revision: ' + revision)
    print('Preserved architecture files:', len(architecture))
    print('Preserved business assets:', len(original_business))


if __name__ == '__main__':
    main()
