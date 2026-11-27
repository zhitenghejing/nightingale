#!/usr/bin/env python3
"""Build offline implementation/acceptance docs; all inputs are local and explicit."""
import argparse
import hashlib
import html
import json
from pathlib import Path
import shutil
import zipfile
from markdown_it import MarkdownIt

HERE = Path(__file__).resolve().parent

def sha(data):
    return hashlib.sha256(data).hexdigest()

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("output", type=Path)
    parser.add_argument("--runtime", required=True, type=Path)
    parser.add_argument("--evidence", required=True, type=Path)
    parser.add_argument("--expires", required=True)
    args = parser.parse_args()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    docs = [(HERE / "implementation-verification.md", "implementation-verification.md", "实现与实际验证"),
            (HERE.parent.parent / "api/ops.md", "ops-api.md", "配置与接口"),
            (HERE / "development-plan.md", "development-plan.md", "开发设计基准"),
            (HERE / "validation-plan.md", "validation-plan.md", "验证设计基准")]
    md = MarkdownIt("commonmark", {"html": False}).enable("table")
    sections = []
    for source, name, title in docs:
        text = source.read_text()
        for old, new in {"../../api/ops.md":"ops-api.md", "../design/alert-knowledge-operations/development-plan.md":"development-plan.md", "../design/alert-knowledge-operations/validation-plan.md":"validation-plan.md", "../design/alert-knowledge-operations/implementation-verification.md":"implementation-verification.md"}.items():
            text = text.replace(old,new)
        (out / name).write_text(text)
        sections.append('<details' + (' open' if len(sections) == 0 else '') + '><summary>' + html.escape(title)
                        + '</summary><article>' + md.render(text) + '</article></details>')
    runtime = json.loads(args.runtime.read_text())
    runtime_text = json.dumps(runtime, ensure_ascii=False, indent=2) + "\n"
    (out / "runtime-verification.json").write_text(runtime_text)
    (out / "runtime-verification.txt").write_text(runtime_text)
    image_names = ["case-proposed.png", "business-blocked.png", "acceptance-case.png", "mobile.png"]
    (out / "images").mkdir(exist_ok=True)
    for name in image_names:
        shutil.copyfile(args.evidence / name, out / "images" / name)
    figures = ''.join('<figure><a href="images/' + name + '"><img loading="lazy" src="images/' + name
                      + '"></a><figcaption>' + label + '</figcaption></figure>' for name, label in zip(image_names,
                      ["取证与待确认操作", "指标恢复但业务失败，处置继续保持未结束", "留待人工验收的新一轮处置", "手机布局检查"]))
    page = '''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>告警知识处置闭环 · 首版实现与验收</title><style>
*{box-sizing:border-box}body{margin:0;background:#f5f7fa;color:#20334b;font:16px/1.75 system-ui,"Noto Sans CJK SC",sans-serif}
header{background:#173b55;color:white;padding:28px max(20px,calc((100vw - 1180px)/2))}header h1{margin:0;font-size:27px}header a{color:white}
main{max-width:1220px;padding:20px;margin:auto}details,section{background:white;border:1px solid #d8e2ed;border-radius:8px;margin:18px 0;padding:20px}
summary{cursor:pointer;font-size:20px;font-weight:600}article{overflow-wrap:anywhere}h1{font-size:26px}h2{font-size:21px;margin-top:30px}
a{color:#13688f;overflow-wrap:anywhere}.flow{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:12px}.flow div{background:#eaf3f8;border-top:4px solid #16749b;padding:15px;font-weight:600}.flow small{display:block;font-weight:400;color:#52677b}
table{border-collapse:collapse;width:100%;font-size:14px}th,td{border:1px solid #d8e2ed;padding:9px;text-align:left;vertical-align:top}th{background:#edf4f9}
pre{overflow:auto;background:#edf3f7;padding:14px}code{font-size:.88em}figure{margin:20px 0}img{display:block;max-width:100%;max-height:720px;border:1px solid #dde4eb}figcaption,.note{color:#52677b;font-size:14px}.links{display:flex;flex-wrap:wrap;gap:18px;margin-top:18px}
@media(max-width:800px){.flow{grid-template-columns:repeat(2,minmax(0,1fr))}main{padding:10px}details,section{padding:12px}table{display:block;overflow-x:auto}}
</style></head><body><header><h1>告警知识处置闭环 · 首版实现与验收</h1><p>功能已实现；完整闭环使用隔离测试应用和脚本化模型验证。真实模型、Ibex、医院环境及人工验收分别报告。</p>
<p>下载有效至 __EXPIRY__。过期后重新生成并校验。</p><div class="links"><a download href="ops-knowledge-implementation.zip">下载完整资料 ZIP</a><a href="SHA256SUMS.txt">文件校验值</a><a href="runtime-verification.txt">运行与验证记录</a><a href="/__preview/info">当前预览构建身份</a><a href="/ops/cases">打开告警处置</a></div></header>
<main><section><div class="flow"><div>告警接入<small>原生事件 / 幂等对账</small></div><div>持续处置<small>身份、目标、人员、进度</small></div><div>知识与取证<small>固定版本 / 授权工具</small></div><div>人员确认<small>固定操作 / 单次消费</small></div><div>恢复验证<small>新鲜指标 + 业务 + 数据</small></div><div>结束与反馈<small>人员结束 / 待审核草稿</small></div></div><p class="note">知识提供方法与标准；处置保存现场事实和证据；Agent 通过受限工具推进；程序与人员分别完成恢复核验和最终确认。</p></section>__DOCS__<section><h2>实际生产 UI 截图</h2>__FIGURES__</section></main></body></html>'''
    page = page.replace("__EXPIRY__", html.escape(args.expires)).replace("__DOCS__", "".join(sections)).replace("__FIGURES__", figures)
    (out / "index.html").write_text(page)
    # Go's ServeFile redirects /index.html to its directory; the SPA router
    # handles that directory. Use an explicit non-index HTML public entry.
    (out / "overview.html").write_text(page)
    files = sorted(p for p in out.rglob("*") if p.is_file() and p.name not in ("SHA256SUMS.txt", "ops-knowledge-implementation.zip"))
    sums = ''.join(sha(p.read_bytes()) + "  " + str(p.relative_to(out)) + "\n" for p in files)
    (out / "SHA256SUMS.txt").write_text(sums)
    bundle = out / "ops-knowledge-implementation.zip"
    with zipfile.ZipFile(bundle, "w", zipfile.ZIP_DEFLATED) as archive:
        for file in [*files, out / "SHA256SUMS.txt"]:
            item = zipfile.ZipInfo(str(file.relative_to(out)), (2026, 11, 7, 0, 0, 0))
            item.external_attr = 0o100644 << 16
            item.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(item, file.read_bytes())
    print(json.dumps({"bytes": bundle.stat().st_size, "sha256": sha(bundle.read_bytes())}))

if __name__ == "__main__":
    main()
