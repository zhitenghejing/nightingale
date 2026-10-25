#!/usr/bin/env python3
"""Package approved vector panels or original-photo viewports and desktop archive.

Original photographs and checksums are preserved as reference evidence.
"""
import argparse
import base64
import csv
import copy
import hashlib
import html
import io
import json
import shutil
import datetime
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FOLDER = ROOT / 'frontend/src/pages/cmdb/materials'
CATALOG = FOLDER / 'catalog.json'


def build():
    catalog = json.loads(CATALOG.read_text())
    imports = []
    references = []
    source_rows = []
    cards = []
    for index, material in enumerate(catalog):
        for side in ('front', 'rear'):
            data = material[side]
            if 'candidate' in data:
                original = ROOT / data['candidate']
            else:
                original = FOLDER / data['original']
            raw = original.read_bytes()
            if hashlib.sha256(raw).hexdigest() != data['sha256']:
                raise RuntimeError(f"Source checksum mismatch: {material['id']}/{side}")
            suffix = original.suffix.lower()
            reference_folder = FOLDER / 'originals' / material['id']
            destination = original if original.parent.resolve() == reference_folder.resolve() else reference_folder / (side + suffix)
            destination.parent.mkdir(parents=True, exist_ok=True)
            if destination.resolve() != original.resolve():
                shutil.copyfile(original, destination)
            data['original'] = str(destination.relative_to(FOLDER))
            data.pop('candidate', None)
            panel = FOLDER / 'panels' / (material['id'] + '-' + side + '.svg')
            panel.parent.mkdir(exist_ok=True)
            if data.get('renderKind') == 'vector':
                source = FOLDER / data['vectorSource']
                vector = source.read_bytes()
                if hashlib.sha256(vector).hexdigest() != data['vectorSha256']:
                    raise RuntimeError(f"Approved vector checksum mismatch: {material['id']}/{side}")
                tree = ET.fromstring(vector)
                if tree.attrib.get('viewBox') != f"0 0 1200 {material['units'] * 120}" or tree.attrib.get('preserveAspectRatio') != 'none' or list(tree.iter('{http://www.w3.org/2000/svg}image')):
                    raise RuntimeError('Vector panel must have the standard U viewBox, preserveAspectRatio=none and no bitmap')
                panel.write_bytes(vector)
            else:
                x, y, width, height = data['viewBox']
                if x < 0 or y < 0 or width <= 0 or height <= 0 or x + width > data['sourceWidth'] or y + height > data['sourceHeight']:
                    raise RuntimeError('Invalid panel viewport')
                mime = {'.jpg': 'image/jpeg', '.png': 'image/png', '.webp': 'image/webp'}[suffix]
                metadata = html.escape(json.dumps({'source': data['sourcePage'], 'image': data['imageUrl'], 'sha256': data['sha256']}, ensure_ascii=False))
                label = html.escape(f"{material['brand']} {material['model']} {'正面' if side == 'front' else '背面'}")
                viewbox = ' '.join(str(v) for v in data['viewBox'])
                svg = f'''<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="{material['units'] * 120}" viewBox="{viewbox}" preserveAspectRatio="none" overflow="hidden">
    <title>{label}</title><metadata>{metadata}</metadata>
    <image x="0" y="0" width="{data['sourceWidth']}" height="{data['sourceHeight']}" href="data:{mime};base64,{base64.b64encode(raw).decode()}"/>
    </svg>\n'''
                panel.write_text(svg)
            data['panel'] = str(panel.relative_to(FOLDER))
            data['panelSha256'] = hashlib.sha256(panel.read_bytes()).hexdigest()
            alias = f'material{index}{side}'
            imports.append(f"import {alias} from './materials/{data['panel']}';")
            references.append((material['id'], side, alias))
            source_rows.append([material['id'], material['brand'], material['model'], material['units'], side, data['sourcePage'], data['imageUrl'], data['sha256'], data['license'], data.get('renderKind', 'photo'), data['panelSha256']])
        cards.append(f'''<article><h2>{html.escape(material['brand'] + ' ' + material['model'])} · {material['units']}U</h2><div class="pair">''' + ''.join(f'''<figure><figcaption>{'正面' if side == 'front' else '背面'}</figcaption><a href="{material[side]['panel']}"><img src="{material[side]['panel']}" style="aspect-ratio:10 / {material['units']}" alt="{html.escape(material['model'])}" loading="lazy"></a><p><a href="{html.escape(material[side]['sourcePage'])}">参考来源</a> · <a href="{material[side]['original']}">参考原图</a></p></figure>''' for side in ('front','rear')) + '</div></article>')
    CATALOG.write_text(json.dumps(catalog, ensure_ascii=False, indent=2) + '\n')
    registry = '\n'.join(imports) + "\n\nexport const materialImages: Record<string, { front: string; rear: string }> = {\n"
    for material in catalog:
        members = ', '.join(f'{side}: {alias}' for key, side, alias in references if key == material['id'])
        registry += f"  '{material['id']}': {{ {members} }},\n"
    registry += '};\n'
    (FOLDER.parent / 'materialImages.ts').write_text(registry)
    output = io.StringIO(newline='')
    writer = csv.writer(output, lineterminator="\n")
    writer.writerow(['素材编号','品牌','型号','标准U数','视图','来源页面','原图地址','原图SHA256','许可说明','渲染方式','面板SHA256'])
    writer.writerows(source_rows)
    (FOLDER / '来源清单.csv').write_text('\ufeff' + output.getvalue())
    page = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>机柜设备素材库</title><style>body{font:15px system-ui;margin:32px;background:#f4f3f8;color:#202330}h1{margin-bottom:8px}article{padding:20px;margin:20px 0;background:white;border-radius:10px}h2{font-size:18px}.pair{display:grid;grid-template-columns:1fr 1fr;gap:24px}figure{margin:0;min-width:0}img{width:100%;object-fit:fill;background:#20252a}figcaption{margin:8px 0}p{color:#6b6b7b}a{color:#6c53b1}</style><h1>机柜设备素材库 · 10 型号 / 20 面板</h1><p>已发布矢量素材：统一视角与绘制方式，保留产品颜色、品牌及面板特点。原始产品图与来源保留为参考。每型号一组正背面，不区分面罩/盘位变体。</p>''' + ''.join(cards) + '</html>\n'
    (FOLDER / 'index.html').write_text(page)
    return catalog


def export(desktop, catalog, replace=False):
    destination = desktop / '素材' / '机柜设备素材库'
    if destination.exists() and replace:
        stamp = datetime.datetime.now().strftime('%Y%m%d-%H%M%S-%f')
        backup = desktop / '素材' / '历史备份' / ('机柜设备素材库-' + stamp)
        backup.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(destination, backup)
        print(json.dumps({'previousDesktopArchive': str(backup)}, ensure_ascii=False))
    destination.mkdir(parents=True, exist_ok=True)
    for material in catalog:
        target = destination / ('交换机' if material['type'] == '交换机' else '服务器') / material['brand'] / material['model']
        target.mkdir(parents=True, exist_ok=True)
        for side, label in [('front','正面'),('rear','背面')]:
            for original, filename in [(FOLDER/material[side]['panel'],label+'.svg'),(FOLDER/material[side]['original'],'原图-'+label+Path(material[side]['original']).suffix)]:
                output = target / filename
                if output.exists() and output.read_bytes() != original.read_bytes() and not replace:
                    raise RuntimeError(f'Refusing to overwrite different existing material: {output}')
                shutil.copyfile(original,output)
        local = copy.deepcopy(material)
        for side, label in [('front','正面'),('rear','背面')]:
            local[side]['panel'] = label+'.svg'
            if local[side].get('renderKind') == 'vector': local[side]['vectorSource'] = label+'.svg'
            local[side]['original'] = '原图-'+label+Path(material[side]['original']).suffix
        (target/'来源.json').write_text(json.dumps(local,ensure_ascii=False,indent=2)+'\n')
    archive = copy.deepcopy(catalog)
    gallery = (FOLDER / 'index.html').read_text()
    for material in archive:
        relative = Path('交换机' if material['type'] == '交换机' else '服务器') / material['brand'] / material['model']
        for side, label in [('front','正面'),('rear','背面')]:
            panel = str(relative / (label+'.svg'))
            original = str(relative / ('原图-'+label+Path(material[side]['original']).suffix))
            gallery = gallery.replace(material[side]['panel'],html.escape(panel)).replace(material[side]['original'],html.escape(original))
            if material[side].get('renderKind') == 'vector': material[side]['vectorSource'] = panel
            material[side]['panel'] = panel
            material[side]['original'] = original
    (destination/'catalog.json').write_text(json.dumps(archive,ensure_ascii=False,indent=2)+'\n')
    (destination/'index.html').write_text(gallery)
    for name in ['来源清单.csv','README.md']:
        path=FOLDER/name
        if path.exists():shutil.copyfile(path,destination/name)
    print(json.dumps({'desktop':str(destination),'models':len(catalog),'panels':len(catalog)*2},ensure_ascii=False))


if __name__ == '__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--desktop',type=Path,help='Optional explicitly authorized desktop directory')
    parser.add_argument('--replace-with-backup', action='store_true', help='Back up an existing desktop library before replacing its panels')
    args=parser.parse_args()
    catalog=build()
    if args.desktop:export(args.desktop,catalog,args.replace_with_backup)
    else:print(json.dumps({'models':len(catalog),'panels':len(catalog)*2,'folder':str(FOLDER)}))
