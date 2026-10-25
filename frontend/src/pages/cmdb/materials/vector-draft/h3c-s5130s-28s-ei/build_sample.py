from pathlib import Path
from xml.sax.saxutils import escape
import json, math
import xml.etree.ElementTree as ET
ROOT=Path(__file__).resolve().parent
logo_root=ET.parse(ROOT/'h3c-logo.svg').getroot()
logo=next(logo_root.iter('{http://www.w3.org/2000/svg}path')).attrib['d']
logo_box=logo_root.attrib['viewBox']
logo_symbol=f'<symbol id="h3c-logo" viewBox="{logo_box}"><path fill="currentColor" fill-rule="evenodd" d="{logo}"/></symbol>'
colors={'chassis':'#303a45','edge':'#83909c','metal':'#aab5bf','socket':'#111820','contact':'#b8a77d','text':'#dae1e7','muted':'#94a3b1','recess':'#1c252f','vent':'#161f28'}
common=f'''<defs>
<linearGradient id="body" x2="0" y2="1"><stop stop-color="#46525e"/><stop offset=".45" stop-color="#35414d"/><stop offset="1" stop-color="#2b3540"/></linearGradient>
<linearGradient id="metal" x2="0" y2="1"><stop stop-color="#c2cbd3"/><stop offset="1" stop-color="#73818e"/></linearGradient>
<pattern id="vents" width="13" height="11.3" patternUnits="userSpaceOnUse"><path d="M3.25 .8 8.8 .8 11.55 5.65 8.8 10.5 3.25 10.5 .5 5.65Z" fill="#19232d" stroke="#56616c" stroke-width=".65"/></pattern>
{logo_symbol}
<g id="screw"><circle r="3.5" fill="#8997a4"/><path d="M-1.7-1.7 1.7 1.7M-1.7 1.7 1.7-1.7" stroke="#303b46" stroke-width="1.1"/></g>
<g id="rj45-core"><path d="M3 3H35V22H27V26H11V22H3Z" fill="#101820" stroke="#576575" stroke-width=".7"/><path d="M6 5H32" stroke="#070c12" stroke-width="3"/>
{''.join(f'<path d="M{x} 6v4" stroke="#b7a684" stroke-width="1.25"/>' for x in range(8,31,3))}<path d="M5 20h5m18 0h5" stroke="#6c7b87" stroke-width="1"/></g>
<g id="rj45"><rect width="38" height="29" rx="1.4" fill="url(#metal)"/><use href="#rj45-core"/></g>
<g id="rj45-up"><rect width="38" height="29" rx="1.4" fill="url(#metal)"/><g transform="translate(0 29) scale(1 -1)"><use href="#rj45-core"/></g></g>
<g id="sfp"><rect width="43" height="28" rx="1.5" fill="url(#metal)"/><rect x="3" y="3" width="37" height="21" rx=".6" fill="#131b24" stroke="#5e6f80" stroke-width=".7"/><path d="M7 6H36M7 20H36" stroke="#30404d" stroke-width="1.5"/><rect x="15" y="25" width="13" height="2" fill="#566575"/></g>
</defs>'''
def frame():
 s='<rect x="1" y="5" width="1198" height="110" rx="3" fill="#26313b" stroke="#667583" stroke-width="1.5"/><rect x="47" y="6" width="1106" height="108" rx="3" fill="url(#body)" stroke="#82909d" stroke-width="1"/><path d="M51 9H1149M51 111H1149" stroke="#a0aeb9" stroke-opacity=".35"/><path d="M46 6V114M1154 6V114" stroke="#111b25" stroke-width="2"/>'
 for x in [23,1177]:
  for y in [28,92]:s+=f'<rect x="{x-7}" y="{y-5}" width="14" height="10" rx="5" fill="#111b25" stroke="#697985" stroke-width="1"/>'
 for x in [58,1142]:
  for y in [17,103]:s+=f'<use href="#screw" x="{x}" y="{y}"/>'
 return s
font='font-family="Arial, sans-serif"'
front=frame()
for bank in range(3):
 x=73+bank*178
 front+=f'<rect x="{x-6}" y="24" width="169" height="76" rx="2" fill="#23303b" stroke="#6e7b88" stroke-width=".8"/>'
 for col in range(4):
  for row in range(2):
   n=bank*8+col*2+row+1;px=x+col*41;py=29+row*36
   socket='rj45-up' if row==0 else 'rj45'
   latch='up' if row==0 else 'down'
   front+=f'<use href="#{socket}" x="{px}" y="{py}" data-port="{n}" data-latch="{latch}"/><text x="{px+19}" y="{21 if row==0 else 108}" text-anchor="middle" fill="#b9c4ce" font-size="8" {font}>{n}</text>'
front+='<rect x="615" y="15" width="257" height="35" rx="2" fill="url(#vents)"/>'
for i in range(4):
 x=622+i*59
 front+=f'<use href="#sfp" x="{x}" y="65"/><text x="{x+21}" y="59" fill="#c2cdd6" font-size="8" text-anchor="middle" {font}>{25+i}</text><circle cx="{x+21}" cy="101" r="2" fill="#96a3af"/><path d="M{x+5} 102h9m15 0h9" stroke="#657686" stroke-width=".8"/>'
front+='<use href="#rj45" x="891" y="63"/><text x="910" y="57" text-anchor="middle" fill="#c7d0d9" font-size="8" '+font+'>CONSOLE</text><path d="M945 78h23l3 4v10h-29V82Z" fill="#121c27" stroke="#a4b0bc" stroke-width="1.4"/><path d="M947 84h19v4h-19Z" fill="#6d7d8b"/><text x="956" y="104" text-anchor="middle" fill="#a8b6c2" font-size="7" '+font+'>USB</text>'
front+='<use href="#h3c-logo" x="1064" y="16" width="66" height="24" color="#e4e9ed"/><text x="981" y="28" fill="#dae2e8" font-size="11" font-weight="600" '+font+'>S5130S</text><rect x="986" y="45" width="145" height="42" rx="2" fill="#293541" stroke="#73818e" stroke-width=".65"/>'
for row in range(2):
 for col in range(12):
  x=995+col*11.6;y=57+row*18
  front+=f'<circle cx="{x:.1f}" cy="{y}" r="2.35" fill="#9dabb7" stroke="#c1cbd3" stroke-width=".45"/>'
front+='<text x="990" y="101" fill="#b9c7d2" font-size="8" '+font+'>S5130S-28S-EI</text><circle cx="1122" cy="99" r="2.5" fill="#9dabb7"/><text x="1122" y="110" fill="#b9c7d2" font-size="6.5" text-anchor="middle" '+font+'>SYS</text>'
rear=frame()
rear+='<circle cx="78" cy="33" r="6" fill="#8996a2" stroke="#c3cbd2"/><path d="M78 28v10m-5-5h10" stroke="#25313d" stroke-width="1.4"/><path d="M78 15v5m-7 0h14m-11 3h8m-6 3h4" stroke="#c1cbd4" stroke-width="1.1"/>'
rear+='<rect x="106" y="29" width="87" height="65" rx="2" fill="#25303a" stroke="#95a3af" stroke-width="1"/><path d="M126 43H174L182 53V83H118V53Z" fill="#101820" stroke="#697887" stroke-width="1.2"/><rect x="147" y="48" width="6" height="16" rx="1" fill="#bfc8cf"/><rect x="128" y="65" width="5" height="13" rx=".8" fill="#bfc8cf"/><rect x="166" y="65" width="5" height="13" rx=".8" fill="#bfc8cf"/><text x="150" y="105" text-anchor="middle" fill="#c8d1d9" font-size="7.5" '+font+'>100–240V~ 50/60Hz 1.0A</text>'
for x in [94,213]:rear+=f'<rect x="{x-9}" y="66" width="18" height="8" rx="4" fill="#8696a4" stroke="#c2cbd3" stroke-width=".7"/><path d="M{x-5} 70h10" stroke="#344452"/>'
for x,w in [(276,91),(391,153),(598,344),(981,108)]:rear+=f'<rect x="{x}" y="23" width="{w}" height="71" rx="1" fill="url(#vents)"/>'
for x in [251,568,960,1116]:rear+=f'<use href="#screw" x="{x}" y="103"/>'
# Logo appears on the physical front only. Rear keeps its original structure.
def svg(content,label):return f'<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="120" viewBox="0 0 1200 120" preserveAspectRatio="none" role="img" aria-label="{escape(label)}"><title>{escape(label)}</title>{common}{content}</svg>'
for side,content in [('front',front),('rear',rear)]: (ROOT/f'{side}.svg').write_text(svg(content,'H3C S5130S-28S-EI '+side+' · vector style draft'))
# A review sheet is separate from transparent, rack-ready assets.
sheet='<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="1680" height="1050" viewBox="0 0 1680 1050"><defs><linearGradient id="bg" x2="1" y2="1"><stop stop-color="#f7f9fc"/><stop offset="1" stop-color="#e8edf4"/></linearGradient></defs><rect width="1680" height="1050" fill="url(#bg)"/>'
def text(x,y,t,size=20,color='#526174',weight='400'):return f'<text x="{x}" y="{y}" font-family="Noto Sans CJK SC, sans-serif" font-size="{size}" fill="{color}" font-weight="{weight}">{escape(t)}</text>'
sheet+=text(72,80,'设备矢量素材库 / 风格样稿 01',18,'#718095')+text(72,136,'H3C S5130S-28S-EI',42,'#1d2b3d','700')+text(73,177,'正交视角 · 统一金属灰阶 · 清晰端口 · 品牌标识保留',21)
sheet+='<rect x="1390" y="92" width="210" height="48" rx="24" fill="#e3eaf1"/>'+text(1420,124,'1U / SVG 矢量',18,'#44576a','600')
for y,label,content,side in [(224,'01  正面 / FRONT',front,'front'),(454,'02  背面 / REAR',rear,'rear')]:
 sheet+=f'<rect x="64" y="{y}" width="1552" height="200" rx="18" fill="#fff" stroke="#dbe3eb"/>'+text(92,y+39,label,17,'#62778c','600')
 panel=svg(content,'H3C vector sample').replace('width="1200" height="120"','x="100" y="'+str(y+66)+'" width="1480" height="120"',1)
 # Independent defs are scoped by unique IDs for each nested panel.
 for id in ['body','metal','vents','h3c-logo','screw','rj45','rj45-core','rj45-up','sfp']:
  panel=panel.replace('id="'+id+'"','id="'+side+'-'+id+'"').replace('#'+id+'"','#'+side+'-'+id+'"').replace('#'+id+')','#'+side+'-'+id+')')
 sheet+=panel
sheet+=text(76,720,'03  统一风格规则',18,'#62778c','600')
for i,(c,t) in enumerate([('#35414d','机身'),('#aab5bf','金属'),('#111820','接口'),('#dae1e7','标识'),('#94a3b1','指示灯')]):
 x=80+i*190;sheet+=f'<rect x="{x}" y="752" width="48" height="48" rx="10" fill="{c}"/>'+text(x+60,782,t,18)
sheet+=text(78,853,'后续服务器沿用相同视角、色阶、线宽与光照；端口、盘位和品牌标识按各型号保留。',20)
sheet+=text(78,897,'面板为透明背景 SVG，可直接缩放放入机柜；指示灯使用中性色，不代表实时运行状态。',19)
sheet+=text(78,975,'结构依据：H3C 官方硬件描述 §2.8  ·  当前仅为风格样稿，尚未替换已发布素材。',16,'#7b8999')
sheet+='</svg>'
(ROOT/'preview.svg').write_text(sheet)
(ROOT/'style.json').write_text(json.dumps({'style':'rack-vector-v1','viewBox':[0,0,1200,120],'units':1,'palette':colors,'brand':'Official H3C mark traced as vector contours; front only','reference':'https://www.h3c.com/cn/d_202602/2753207_30005_0.htm#2.8','status':'draft-awaiting-user-style-review'},ensure_ascii=False,indent=2))
print('Created front.svg, rear.svg, official logo paths and review sheet.')
