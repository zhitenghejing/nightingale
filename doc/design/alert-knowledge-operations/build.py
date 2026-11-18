#!/usr/bin/env python3
"""Build an offline reading page and deterministic download bundle for the plan."""

import argparse
import hashlib
import html
from pathlib import Path
import re
import zipfile

from markdown_it import MarkdownIt


HERE = Path(__file__).resolve().parent
FILES = ("development-plan.md", "validation-plan.md")


def digest(data):
    return hashlib.sha256(data).hexdigest()


def diagram():
    steps = (
        ("告警接入", "已落库事件 · 幂等与对账"),
        ("持续处置", "人员接手 · 数据库保存进度"),
        ("Agent 取证", "受限工具 · 真实来源与时间"),
        ("方法与执行", "知识版本 · 人员确认操作"),
        ("恢复验证", "指标 + 业务 + 数据核对"),
        ("结束与反馈", "人员确认 · 修订知识草稿"),
    )
    nodes = "".join(
        '<div class="flow-step"><strong>' + title + '</strong><span>'
        + detail + '</span></div>' for title, detail in steps
    )
    return ('<figure aria-label="告警处置业务和信息流"><div class="flow">'
            + nodes + '</div><figcaption>知识模块提供方法；处置模块保存事实、步骤、审批和证据。'
            'Agent 的运行结果必须通过程序校验，正式知识由负责人审核发布。'
            '</figcaption></figure>')


def render(source, prefix):
    md = MarkdownIt("commonmark", {"html": False}).enable("table")
    tokens = md.parse(source)
    headings = []
    count = 0
    for pos, token in enumerate(tokens):
        if token.type == "heading_open":
            count += 1
            ident = f"{prefix}-{count}"
            token.attrSet("id", ident)
            if token.tag == "h2":
                headings.append((ident, tokens[pos + 1].content))
        elif token.type == "link_open":
            href = token.attrGet("href") or ""
            if href in FILES:
                token.attrSet("href", "#" + ("development" if href == FILES[0] else "validation"))
    original = md.renderer.rules.get("fence")

    def fence(items, idx, options, env):
        if items[idx].info.strip() == "mermaid":
            return diagram()
        if original:
            return original(items, idx, options, env)
        return md.renderer.renderToken(items, idx, options, env)

    md.renderer.rules["fence"] = fence
    return md.renderer.render(tokens, md.options, {}), headings


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("output", type=Path)
    parser.add_argument("--expires", required=True, help="UTC expiration shown on the page")
    args = parser.parse_args()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    sections, links = [], []
    for name, prefix, label in (
        (FILES[0], "development", "开发实现方案"),
        (FILES[1], "validation", "验证方案"),
    ):
        source = (HERE / name).read_text()
        (out / name).write_text(source)
        body, headings = render(source, prefix)
        sections.append(f'<section id="{prefix}">{body}</section>')
        links.append('<details open><summary>' + label + '</summary><ol>'
                     + ''.join('<li><a href="#' + ident + '">' + html.escape(text)
                               + '</a></li>' for ident, text in headings)
                     + '</ol></details>')
    page = '''<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>告警知识处置闭环 开发与验证方案</title>
<style>
:root{color-scheme:light;--ink:#213247;--sub:#52657a;--line:#d9e2ea;--accent:#135f8c}
*{box-sizing:border-box}body{margin:0;background:#f5f7fa;color:var(--ink);font:16px/1.8 system-ui,"Noto Sans CJK SC",sans-serif}
header{background:#173b55;color:#fff;padding:32px max(24px,calc((100vw - 1320px)/2))}
header h1{font-size:28px;margin:0 0 10px}header p{margin:8px 0;color:#dbe8f0}
.download{display:flex;flex-wrap:wrap;gap:12px;margin-top:18px}.download a{color:#fff;border:1px solid #9ab9cd;border-radius:5px;padding:6px 14px;text-decoration:none}
.layout{display:grid;grid-template-columns:250px minmax(0,1fr);gap:30px;max-width:1360px;margin:26px auto;padding:0 24px 60px}
nav{align-self:start;position:sticky;top:20px;max-height:90vh;overflow:auto;padding:12px 14px;background:#fff;border:1px solid var(--line);border-radius:6px;font-size:14px}
nav ol{margin:10px 0;padding-left:20px}nav li{margin:5px 0}nav summary{font-weight:600;cursor:pointer}
main{min-width:0;background:#fff;padding:24px 30px;border:1px solid var(--line);border-radius:6px}
section+section{border-top:3px solid var(--line);margin-top:60px;padding-top:25px}
h1{font-size:27px;line-height:1.5}h2{font-size:22px;margin-top:36px}h3{font-size:18px;margin-top:26px}h1,h2,h3,section{scroll-margin-top:22px}
a{color:var(--accent);overflow-wrap:anywhere}p,li,td{overflow-wrap:anywhere}code{font-size:.9em;background:#eef3f7;padding:2px 4px;border-radius:3px}pre{overflow:auto;background:#eef3f7;padding:16px}
table{border-collapse:collapse;width:100%;font-size:14px;margin:18px 0}th,td{text-align:left;vertical-align:top;border:1px solid var(--line);padding:10px}th{background:#edf3f7}tr:nth-child(even) td{background:#fafcfd}
figure{margin:24px 0}.flow{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}.flow-step{padding:14px;background:#eaf3f8;border-left:4px solid var(--accent);min-width:0}.flow-step strong,.flow-step span{display:block}.flow-step span,figcaption{font-size:13px;color:var(--sub)}figcaption{margin-top:12px}
.notice{font-size:14px}footer{color:var(--sub);font-size:13px;margin-top:40px}
@media(max-width:950px){.layout{grid-template-columns:1fr}nav{position:static;max-height:none}main{padding:16px}nav details:not(:first-child){margin-top:12px}}
@media(max-width:600px){header h1{font-size:23px}.layout{padding:0 12px 35px;gap:15px}.flow{grid-template-columns:1fr}table{font-size:12px}th,td{padding:6px}h1{font-size:23px}h2{font-size:20px}}
@media print{header{background:none;color:#000;padding:0}header p{color:#333}nav,.download{display:none}.layout{display:block;margin:0;padding:0}main{border:0;padding:0}table{font-size:10px}a{color:#000}}
</style></head><body>
<header><h1>告警知识处置闭环 开发与验证方案</h1>
<p>自动取证与辅助判断 → 人员确认执行 → 按证据验证恢复 → 经验形成知识草稿</p>
<p class="notice">v1.0 · 代码基线 2ba46536 · 设计交付，业务功能尚未实施</p>
<p class="notice">临时入口有效期截至 __EXPIRES__；预览提前停止时入口也会失效。</p>
<div class="download"><a download href="alert-knowledge-operations-v1.zip">下载完整方案 ZIP</a>
<a download href="development-plan.md">开发方案 Markdown</a><a download href="validation-plan.md">验证方案 Markdown</a>
<a download href="index.html">离线阅读 HTML</a><a href="SHA256SUMS.txt">查看 SHA-256</a></div></header>
<div class="layout"><nav aria-label="方案目录">__NAV__</nav><main>__BODY__
<footer>资料仅含开发设计与测试设计，不含账号、密钥、患者数据或生产运行记录。测试预期与实际结果分别记录。</footer>
</main></div></body></html>'''
    page = page.replace("__EXPIRES__", html.escape(args.expires))
    page = page.replace("__NAV__", "".join(links)).replace("__BODY__", "".join(sections))
    (out / "index.html").write_text(page)
    entries = [*FILES, "index.html"]
    checksums = ''.join(digest((out / name).read_bytes()) + '  ' + name + '\n' for name in entries)
    (out / "SHA256SUMS.txt").write_text(checksums)
    bundle = out / "alert-knowledge-operations-v1.zip"
    with zipfile.ZipFile(bundle, "w", zipfile.ZIP_DEFLATED) as archive:
        for name in [*entries, "SHA256SUMS.txt"]:
            info = zipfile.ZipInfo(name, (2026, 11, 6, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, (out / name).read_bytes())
    print("Built", out)
    print("Bundle bytes", bundle.stat().st_size)
    print("Bundle SHA-256", digest(bundle.read_bytes()))


if __name__ == "__main__":
    main()
