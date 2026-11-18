#!/usr/bin/env python3
"""Build offline verification docs; accepts only explicitly selected screenshots."""
import argparse
import hashlib
import html
import json
from pathlib import Path
import shutil
import zipfile

from markdown_it import MarkdownIt


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('output', type=Path)
    p.add_argument('--screenshots', required=True, type=Path)
    p.add_argument('--expires', required=True)
    args = p.parse_args()
    source = Path(__file__).resolve().parent
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    files = []
    for name in ('validation-plan.md', 'test-results.md', 'review.md'):
        shutil.copyfile(source / name, output / name)
        files.append(name)
    shutil.copyfile(source.parents[1] / 'api' / 'environment.md', output / 'environment-api.md')
    files.append('environment-api.md')
    for name in ('target-relations.png', 'document-candidates.png', 'mobile-relations.png'):
        shutil.copyfile(args.screenshots / name, output / name)
        files.append(name)
    renderer = MarkdownIt('commonmark', {'html': False}).enable('table')
    body = renderer.render((source / 'test-results.md').read_text())
    plan = renderer.render((source / 'validation-plan.md').read_text())
    page = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>环境关联开发与实际验证</title><style>
*{box-sizing:border-box}body{margin:0;background:#f7f8fb;color:#20242c;font:16px/1.75 system-ui,"Microsoft YaHei",sans-serif}main{max-width:1160px;margin:24px auto;padding:32px;background:white;border:1px solid #e6e8ee;border-radius:12px}h1{font-size:28px}h2{margin-top:36px;color:#58438f}a{color:#7250b4}nav{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:24px}nav a{padding:8px 14px;border:1px solid #d9d1ee;border-radius:6px;text-decoration:none}nav a:first-child{background:#7250b4;color:white}.expiry{padding:12px;background:#f2edf9;border-radius:6px}img{max-width:100%;height:auto;border:1px solid #eee;border-radius:8px}table{border-collapse:collapse;display:block;overflow:auto;font-size:14px}th,td{border:1px solid #e1e3e8;padding:10px;min-width:75px;text-align:left;vertical-align:top}th{background:#f6f4fa}pre{overflow:auto;background:#f6f7fa;padding:12px}code{overflow-wrap:anywhere}details{margin-top:40px;border-top:1px solid #eee;padding-top:18px}@media(max-width:600px){main{margin:8px;padding:16px}h1{font-size:23px}body{font-size:15px}}
</style><main><nav><a href="environment-verification.zip" download>下载完整验证资料</a><a href="test-results.md" download>实际测试报告</a><a href="validation-plan.md" download>开发前预期测试方案</a><a href="checksums.json">文件校验清单</a></nav>'''
    page += '<p class="expiry">下载有效至 ' + html.escape(args.expires) + '。仅含方案、验证记录和模拟场景截图，不含账号凭据或客户原文。</p>'
    page += body + '<details><summary>展开开发前预期测试方案</summary>' + plan + '</details></main></html>'
    (output / 'overview.html').write_text(page)
    files.append('overview.html')
    checks = {name: {'bytes': (output / name).stat().st_size, 'sha256': hashlib.sha256((output / name).read_bytes()).hexdigest()} for name in files}
    (output / 'checksums.json').write_text(json.dumps({'expires': args.expires, 'files': checks}, ensure_ascii=False, indent=2))
    files.append('checksums.json')
    with zipfile.ZipFile(output / 'environment-verification.zip', 'w', compression=zipfile.ZIP_DEFLATED) as z:
        for name in sorted(files):
            info = zipfile.ZipInfo(name, (2026, 11, 18, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, (output / name).read_bytes())
    archive = output / 'environment-verification.zip'
    print(json.dumps({'bytes': archive.stat().st_size, 'sha256': hashlib.sha256(archive.read_bytes()).hexdigest(), 'files': len(files)}, ensure_ascii=False))


if __name__ == '__main__':
    main()
