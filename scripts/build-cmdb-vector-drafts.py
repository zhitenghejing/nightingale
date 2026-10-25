#!/usr/bin/env python3
"""Build review-only native SVG assets; never changes the published catalog."""
from pathlib import Path
from xml.sax.saxutils import escape
import xml.etree.ElementTree as ET
import json, math
ROOT=Path(__file__).resolve().parents[1]
MATERIALS=ROOT/'frontend/src/pages/cmdb/materials'
OUT=MATERIALS/'vector-draft'
NS='{http://www.w3.org/2000/svg}'
CATALOG=json.loads((MATERIALS/'catalog.json').read_text())

def rect(x,y,w,h,fill,stroke=None,rx=1,sw=1):
 return f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill}"'+(f' stroke="{stroke}" stroke-width="{sw}"' if stroke else '')+'/>'
def path(d,fill='none',stroke=None,sw=1):return f'<path d="{d}" fill="{fill}"'+(f' stroke="{stroke}" stroke-width="{sw}"' if stroke else '')+'/>'
def text(x,y,label,size=12,color='#c5ccd2',weight='400',anchor='start'):
 return f'<text x="{x}" y="{y}" font-family="Arial, Noto Sans CJK SC, sans-serif" font-size="{size}" font-weight="{weight}" text-anchor="{anchor}" fill="{color}">{escape(str(label))}</text>'
def use(id,x=0,y=0,w=None,h=None):return f'<use href="#{id}" x="{x}" y="{y}"'+(f' width="{w}" height="{h}"' if w is not None else '')+'/>'
def circle(x,y,r,fill,stroke=None,sw=1):return f'<circle cx="{x}" cy="{y}" r="{r}" fill="{fill}"'+(f' stroke="{stroke}" stroke-width="{sw}"' if stroke else '')+'/>'

def brand_symbol(brand):
 if brand=='hpe':return '<symbol id="brand" viewBox="0 0 150 36"><path d="M0 0H150V36H0ZM7 7V29H143V7Z" fill="currentColor" fill-rule="evenodd"/></symbol>'
 r=ET.parse(OUT/'brand-marks'/f'{brand}.svg').getroot()
 box=r.attrib['viewBox']
 shapes=[]
 for e in r.iter(NS+'path'):
  d=e.attrib['d']
  if brand=='dell':
   d=d.split('M11.82 0')[0];box='3 8 19 8'
  if brand=='lenovo':
   d=d.replace('M24 7.997v8.006H0V7.997h24z','');box='2 9 20 6'
  fill='#e2e6e8' if brand=='dell' else 'currentColor'
  shapes.append(f'<path d="{d}" fill="{fill}" fill-rule="evenodd"/>')
 return f'<symbol id="brand" viewBox="{box}">{"".join(shapes)}</symbol>'

def defs(brand):
 return '''<defs>
<linearGradient id="dark" x2="0" y2="1"><stop stop-color="#424448"/><stop offset=".48" stop-color="#25282d"/><stop offset="1" stop-color="#1a1e23"/></linearGradient>
<linearGradient id="silver" x2="0" y2="1"><stop stop-color="#e3e5e7"/><stop offset=".45" stop-color="#b5bbc0"/><stop offset="1" stop-color="#90999f"/></linearGradient>
<linearGradient id="blue-metal" x2="0" y2="1"><stop stop-color="#71828e"/><stop offset=".5" stop-color="#435867"/><stop offset="1" stop-color="#2d414f"/></linearGradient>
<pattern id="mesh" width="9" height="8" patternUnits="userSpaceOnUse"><path d="M2.4 .6H6.3L8.3 4 6.3 7.4H2.4L.4 4Z" fill="#11171d" stroke="#5e676f" stroke-width=".65"/></pattern>
<pattern id="tiny" width="7" height="6" patternUnits="userSpaceOnUse"><rect x="1" y="1" width="4.5" height="2.4" rx=".6" fill="#11171b"/></pattern>
<pattern id="rear-hole" width="9" height="8" patternUnits="userSpaceOnUse"><rect x="1" y="1" width="5.7" height="5.1" rx="1.4" fill="#303940"/></pattern>
<g id="screw"><circle r="4" fill="#b9c0c5" stroke="#657078" stroke-width=".7"/><path d="M-2-2 2 2M-2 2 2-2" stroke="#69737a" stroke-width="1"/></g>
<g id="nic"><rect width="34" height="27" rx="1" fill="#c5cbd0" stroke="#7b858e"/><path d="M3 3H31V20H24V24H10V20H3Z" fill="#12191f"/><path d="M7 6v4m3-4v4m3-4v4m3-4v4m3-4v4m3-4v4m3-4v4" stroke="#b1a27b" stroke-width="1.4"/></g>
<g id="usb"><rect width="30" height="15" rx="1" fill="#c6ccd1" stroke="#616e79"/><rect x="3" y="3" width="24" height="9" fill="#17212c"/><rect x="5" y="8" width="20" height="3" fill="#3c7baf"/></g>
<g id="vga"><path d="M4 0H42L46 4 43 17H3L0 4Z" fill="#4c75ac" stroke="#aab6c1" stroke-width="2"/><path d="M7 4H39L37 13H9Z" fill="#405a83"/>
'''+''.join(circle(10+c*6+(r%2)*2,5+r*3,0.9,'#d9dfdf') for r in range(3) for c in range(5))+'''</g>
'''+brand_symbol(brand)+'</defs>'

def frame(h,back=False,blue=False):
 fill='url(#silver)' if back else 'url(#blue-metal)' if blue else 'url(#dark)'
 s=rect(1,5,1198,h-10,'#242a2f','#717980',3)+rect(48,6,1104,h-12,fill,'#939ca3',2)
 for x in [20,1180]:
  for y in [26,h-26]:s+=rect(x-7,y-5,14,10,'#121a21','#6a7680',5)
 for x in [58,1142]:
  for y in [17,h-17]:s+=use('screw',x,y)
 return s

def badge(brand,x,y,w,h,color='#e2e6e8'):
 return f'<g color="{color}">{use("brand",x,y,w,h)}</g>'

def drive(x,y,w,h,kind='sff',accent='#69757c',index=None):
 # Preserve vertical SFF, horizontal LFF, release catches and metal carrier faces.
 s=rect(x,y,w,h,'#171d21','#69727a',2)+rect(x+3,y+3,w-6,h-6,'#242a2f','#343d44',1)
 if kind=='lff':
  s+=rect(x+8,y+7,w-80,h-14,'url(#mesh)','#586872',1)
  s+=path(f'M{x+w-69} {y+5}L{x+w-40} {y+11}V{y+h-11}L{x+w-69} {y+h-5}Z','url(#blue-metal)','#8f9da6',1)
  s+=rect(x+w-35,y+6,27,h-12,'#2e3c46','#52616c',1)
  s+=path(f'M{x+12} {y+h-5}H{x+w-76}',stroke='#a7b1b8',sw=1)
 elif kind=='rear':
  s+=rect(x+7,y+5,w-46,h-10,'url(#tiny)','#545d65',1)+rect(x+w-34,y+6,24,h-12,'#363039',accent,1)
 else:
  s+=rect(x+6,y+18,w-12,max(8,h-45),'url(#mesh)','#53606b',1)
  s+=rect(x+6,y+5,w-12,9,'#555d63',rx=1)
  s+=rect(x+6,y+h-22,w-12,15,'#343d45',accent,2)
 if index is not None:s+=text(x+w/2,y-4,index,8,'#a4adb5',anchor='middle')
 return s

def fan(cx,cy,r=30,accent='#a0aab1'):
 s=circle(cx,cy,r,'#151c22',accent,1.5)
 for i in range(7):
  a=i*2*math.pi/7;b=a+.48
  x1=cx+math.cos(a)*8;y1=cy+math.sin(a)*8;x2=cx+math.cos(a+.18)*(r-4);y2=cy+math.sin(a+.18)*(r-4);x3=cx+math.cos(b)*(r-6);y3=cy+math.sin(b)*(r-6)
  s+=path(f'M{x1:.2f} {y1:.2f}Q{x2:.2f} {y2:.2f} {x3:.2f} {y3:.2f}L{cx} {cy}Z','#87949d')
 return s+circle(cx,cy,9,'#b8c1c7','#343e46',1)

def psu(x,y,w,h,accent='#e68b32',label='800W'):
 r=min(h*.38,w*.26)
 s=rect(x,y,w,h,'#303a42','#8b959c',2)+fan(x+r+6,y+h/2,r-2)
 # IEC C14 inlet and the model-specific release handle.
 px=x+w-45;py=y+h/2-21
 s+=rect(px-5,py-6,43,49,'#202930','#4c5963',2)
 s+=path(f'M{px+4} {py}H{px+23}L{px+28} {py+7}V{py+34}H{px-1}V{py+7}Z','#0b1116','#78858e',1)
 for dx,dy in [(8,7),(2,21),(16,21)]:s+=rect(px+dx,py+dy,3,9,'#c4cbd0',rx=.5)
 s+=rect(x+w-9,y+13,6,h-26,accent,rx=2)
 s+=text(x+8,y+10,label,6.5,'#dbe1e5')
 return s

def pci(x,y,w,count,slot_h=24,accent='#8aa1b3'):
 s=''
 for n in range(count):
  yy=y+n*(slot_h+5)
  s+=rect(x,yy,w,slot_h,'#a9b1b6','#75828a',1)
  s+=rect(x+15,yy+5,w-40,slot_h-10,'url(#rear-hole)',rx=1)+use('screw',x+w-10,yy+slot_h/2)
 s+=rect(x+w-3,y,5,count*(slot_h+5)-5,accent,rx=1)
 return s

def ports(x,y,n=4,gap=39):
 return ''.join(use('nic',x+i*gap,y) for i in range(n))

def front_hpe(m):
 h=m['units']*120;g11='gen11' in m['id'];s=frame(h)
 # Dark endcaps and unmistakable HPE silver shoulders.
 for x in [49,1095]:s+=rect(x,12,56,h-24,'#24282b','#5b6268',2)
 left=f'M107 12H250Q270 12 302 30L169 45Q153 49 153 65V{h-65}Q153 {h-49} 169 {h-45}L302 {h-30}Q270 {h-12} 250 {h-12}H107Z'
 s+=path(left,'url(#silver)','#c3c9ce',1)+path(f'M1093 12H950Q930 12 898 30L1031 45Q1047 49 1047 65V{h-65}Q1047 {h-49} 1031 {h-45}L898 {h-30}Q930 {h-12} 950 {h-12}H1093Z','url(#silver)','#c3c9ce',1)
 if g11:
  s+=rect(126,27,949,h-54,'#171c20','#7e858b',2)
  for i in range(24):
   x=135+i*38
   s+=rect(x,34,30,44,'#293035','#5b666e',1)+rect(x,h-78,30,44,'#293035','#5b666e',1)
   s+=rect(x+4,39,22,12,'url(#tiny)',rx=0)+rect(x+4,h-58,22,12,'url(#tiny)',rx=0)
  for i in range(44):s+=rect(141+i*21,82,10,h-164,'#0e1419',rx=1)
  # The two broad diagonals are the Gen11 bezel signature.
  s+=path(f'M128 73L1072 {h-73}M128 {h-73}L1072 73',stroke='#aeb7bd',sw=12)
 else:
  s+=path(f'M135 35Q160 24 225 24H975Q1040 24 1065 35V{h-35}Q1040 {h-24} 975 {h-24}H225Q160 {h-24} 135 {h-35}Z','url(#tiny)','#515b62',1)
  for y in range(35,h-28,14):s+=path(f'M145 {y}H1055',stroke='#50575c',sw=.8)
  # Lower drive-release details remain visible beneath the perforation.
  for i in range(24):s+=rect(156+i*37,h-40,21,6,'#383940','#5a4b55',1)
 s+=rect(521,h/2-23,158,47,'#14201f','#44514d',1)+badge('hpe',534,h/2-14,132,28,'#00b388')
 s+=text(77,h-20,'HPE',8,'#e2e6e8','600','middle')+text(1123,h-26,'ProLiant',7,'#d0d7dd',anchor='middle')
 for i in range(4):s+=circle(1124,27+i*12,2.8,'#93a89f' if i<2 else '#7d9aab')
 s+=rect(1114,86 if h>120 else 76,20,10,'#12191e','#7b848b',1)
 s+=text(75,44,'DL380' if h==240 else 'DL360',7,'#aeb7bf',anchor='middle')
 return s

def front_dell(m):
 h=m['units']*120;s=frame(h);model=m['model'].split()[-1]
 for x in [49,1099]:s+=rect(x,11,52,h-22,'#24272b','#5c646c',2)
 if model=='R740':
  # User-approved 14G bezel reference: staggered large hexagons over a square
  # intake grid, warm silver ribs, left lock and the narrow right I/O column.
  s+='<defs><pattern id="r740-grid" width="11" height="11" patternUnits="userSpaceOnUse"><rect width="11" height="11" fill="#252c32"/><rect x="1.6" y="1.6" width="7.8" height="7.8" rx=".6" fill="#12181d"/></pattern><clipPath id="r740-bezel"><rect x="137" y="12" width="934" height="216" rx="2"/></clipPath><linearGradient id="r740-rib" x2="0" y2="1"><stop stop-color="#c5c0b7"/><stop offset=".5" stop-color="#a5a198"/><stop offset="1" stop-color="#8a8984"/></linearGradient></defs>'
  s+=rect(137,12,934,216,'url(#r740-grid)','#52585c',2)
  s+='<g clip-path="url(#r740-bezel)" data-part="14g-honeycomb-bezel">'
  # Measured from the approved photo, in its original pixel coordinates.
  # Five complete middle cells; four top/bottom cells and clipped end cells.
  # Coordinates are normalized to the front bezel, not an arbitrary grid.
  sx=1200/1535;sy=216/251
  reference_rows=[('middle',995,[312,558,804,1050,1296]),
                  ('top',922,[189,435,681,927,1173,1419]),
                  ('bottom',1068,[189,435,681,927,1173,1419])]
  vertices=[(-82,0),(-41,-73),(41,-73),(82,0),(41,73),(-41,73)]
  for row,cy,centers in reference_rows:
   for cx in centers:
    points=[((cx+dx-32)*sx,12+(cy+dy-861)*sy) for dx,dy in vertices]
    d='M'+'L'.join(f'{x:.2f} {y:.2f}' for x,y in points)+'Z'
    s+=f'<g data-cell-row="{row}" data-reference-center="{cx},{cy}">'+path(d,stroke='#4c4e4d',sw=15)+path(d,stroke='url(#r740-rib)',sw=12)+'</g>'
  s+='</g>'
  s+=rect(69,52,58,134,'#2e3337','#555c61',2)
  s+=circle(184,67,16,'#454b4f','#a7aaa8',2)+circle(184,67,9,'#272d30','#727a7c',1)
  s+=path('M182 62h4v9h-4Z','#111719')
  s+=rect(488,108,226,40,'#22282c',rx=2)+badge('dell',494,113,116,31)+text(613,140,'EMC',30,'#e1e4e6')
  s+=path('M1097 15V225',stroke='#b6bab9',sw=2)
  s+=circle(1125,27,4,'#84958c','#b8c2bd',1)
  s+='<g transform="translate(1124 55) rotate(90)">'+use('usb')+'</g>'
  s+='<g transform="translate(1145 52) rotate(90)">'+use('vga')+'</g>'
  s+='<g transform="translate(1124 120) rotate(90)">'+use('usb')+'</g>'
  s+=rect(1111,171,11,21,'#111c24','#9ba5ae',1)+rect(1114,176,5,11,'#526879',rx=.5)
  s+=rect(1033,217,65,12,'#181f25','#4b555d',2)+text(1064,226,'R740',7,'#c5cdd2',anchor='middle')
  return s
 else:
  count=12 if h==240 else 10
  for i in range(count):
   x=110+i*(974/count)
   s+=rect(x,24,974/count-5,h-48,'#11171b','#434b52',1)
   for yy in ([38,h-85] if h==240 else [36]):s+=rect(x+7,yy,974/count-19,48 if h==240 else 45,'url(#mesh)','#4b555d',1)
  s+=rect(113,25,966,h-50,'url(#mesh)','#636b72',2)
  # Dell 15G silver hexagonal bezel; R650 keeps a lower, single-row lattice.
  if h==240:
   for i in range(7):
    x=120+i*135
    s+=path(f'M{x+22} 32H{x+80}L{x+112} 77 {x+80} 122H{x+22}L{x-10} 77Z M{x+22} 122H{x+80}L{x+112} 167 {x+80} 212H{x+22}L{x-10} 167Z',stroke='#aeb3b7',sw=8)
  else:
   for i in range(7):
    x=122+i*135;s+=path(f'M{x} 26L{x+25} 59H{x+79}L{x+108} 26M{x} 94L{x+25} 61H{x+79}L{x+108} 94',stroke='#aeb3b7',sw=7)
  s+=rect(467,h/2-25,267,50,'#252b30',rx=2)+badge('dell',482,h/2-17,139,34)+text(624,h/2+12,'EMC',31,'#dde2e6','400')
  s+=circle(170,h/2,16,'#464f56','#8b949a',2)+path(f'M165 {h/2-2}h10m-5-5v10',stroke='#20292f',sw=2)
  s+=rect(796,18,149,13,'#565d63',rx=1)+text(802,28,'PowerEdge '+model,8,'#c6cdd3')
 s+=rect(70,23,7,38,'#285b82','#5985a5',1)+text(1125,h-20,model,8,'#b9c1c8',anchor='middle')
 s+=circle(1124,29,5,'#77907c','#c0cdc3',1)+rect(1116,50,16,29,'#131c24','#8c969f',1)
 s+=use('usb',1109,h-62)
 return s

def front_lenovo(m):
 h=240;s=frame(h)
 for i in range(8):
  x=111+i*41;s+=drive(x,29,37,182,'sff','#807b75',i)
 for x,w in [(453,323),(790,287)]:
  s+=rect(x,28,w,184,'url(#mesh)','#4e595f',2)
  for y in [54,151]:s+=rect(x+w-83,y,61,34,'#171c20','#525d65',2)+rect(x+w-69,y+11,34,11,'#232a31',rx=1)
 s+=path('M55 18H1145M55 222H1145',stroke='#9a9694',sw=2)
 s+=f'<g transform="translate(65 216) rotate(-90)">{badge("lenovo",0,0,81,24,"#e5e7e8")}</g>'
 s+=text(1124,190,'SR650 V2',7.5,'#d3d8dd',anchor='middle')+text(1124,202,'ThinkSystem',6,'#b9c0c7',anchor='middle')
 s+=use('usb',1109,80)+use('usb',1109,103)+circle(1123,34,5,'#a2aa9f','#d1d7d4')
 return s

def front_huawei(m):
 h=240;s=frame(h)
 for i in range(8):s+=drive(110+i*43,30,39,182,'sff','#6c7564',i)
 s+=rect(462,30,620,182,'url(#mesh)','#647078',2)
 s+=badge('huawei',60,153,43,51,'#dfe3e5')+use('usb',65,39)+use('usb',65,62)
 s+=text(1124,39,'2288',13,'#d1d6d9','600','middle')+text(1124,57,'H V5',8,'#bfc9ce',anchor='middle')
 s+=rect(1109,75,30,10,'#3e484f','#8e999f',2)+rect(1112,96,24,24,'#4c585f','#89959b',2)+circle(1124,108,5,'#768a7d')
 s+=rect(1112,171,25,30,'#2f70a0','#92b7cb',2)+text(1124,190,'Xeon',6,'#d9e8f2',anchor='middle')
 return s

def front_inspur(m):
 h=240;s=frame(h,blue=True)
 for row in range(3):
  for col in range(4):s+=drive(117+col*239,25+row*63,232,58,'lff','#8b9ba6',row*4+col)
 s+=badge('inspur',55,16,53,17,'#e2eaf0')+circle(81,61,10,'#203846','#a2b5c2',1.5)+path('M81 53v8',stroke='#adbfcb',sw=1.5)
 for y in [93,111,129]:s+=rect(66,y,9,4,'#8aabbc',rx=1)
 s+=rect(60,160,41,40,'#314d60','#7893a3',2)+text(81,184,'i',22,'#bbcbd6','600','middle')
 s+=use('vga',1101,49)+use('usb',1108,111)+use('usb',1108,131)+text(1124,213,'M6',13,'#cbd8e0','600','middle')
 return s

def rear_hpe(m):
 h=m['units']*120;s=frame(h,True)
 if h==120:
  s+=pci(82,16,375,2,24,'#a5a7a9')+pci(474,16,339,2,24,'#a5a7a9')
  s+=psu(837,20,150,81,'#885777','500W')+psu(995,20,143,81,'#885777','500W')
  s+=use('usb',89,83)+use('usb',123,83)+use('vga',216,82)+ports(295,78,1)+ports(507,78,1)+use('vga',734,82)
 else:
  g11='gen11' in m['id']
  if g11:s+=pci(90,22,355,3,31,'#a1a5a6')+pci(458,22,330,3,31,'#a1a5a6')
  else:
   for x in [96,451]:
    s+=drive(x,25,295,44,'rear','#946183')+drive(x,75,295,44,'rear','#946183')+pci(x,133,295,1,28,'#a7acaf')
  s+=drive(827,22,300,39,'rear','#946183')+drive(827,67,300,39,'rear','#946183')
  s+=psu(827,125,149,94,'#8c567c','800W')+psu(986,125,149,94,'#8c567c','800W')
  s+=ports(102,183,4)+use('usb',279,181)+use('usb',279,201)+ports(449,183,1)+ports(513,183,4)+use('vga',731,188)
 return s

def rear_dell(m):
 model=m['model'].split()[-1];h=m['units']*120;s=frame(h,True)
 if model=='R650':
  s+=psu(72,19,153,82,'#d68c46','800W')+psu(991,19,144,82,'#d68c46','800W')
  for x in [252,444]:s+=drive(x,15,182,35,'rear','#a59778')
  s+=pci(649,18,309,1,33,'#668ba7')+ports(286,76,1)+ports(470,76,4)+use('usb',695,75)+use('vga',844,77)
 elif model=='R750':
  for x in [94,327]:
   s+=drive(x,22,220,38,'rear','#a99a79')+drive(x,66,220,38,'rear','#a99a79')
  s+=pci(617,23,501,3,27,'#5b8eaf')
  s+=rect(557,25,46,78,'url(#rear-hole)','#808b94',1)
  s+=psu(89,126,218,91,'#e79c47','1400W')+psu(925,126,205,91,'#e79c47','1400W')
  s+=ports(361,183,1)+ports(531,183,4)+use('usb',747,180)+use('vga',835,184)
  s+=pci(341,129,555,1,31,'#738da0')
 else:
  s+=pci(90,23,346,3,33,'#6b9abb')+pci(455,23,326,3,33,'#6b9abb')+pci(803,23,324,2,33,'#6b9abb')
  s+=psu(823,127,152,91,'#e59943','750W')+psu(985,127,150,91,'#e59943','750W')
  s+=ports(94,184,1)+use('vga',248,189)+use('usb',324,180)+use('usb',324,200)+ports(392,184,1)+ports(504,184,4)
 return s

def rear_lenovo(m):
 s=frame(240,True)+pci(91,22,345,3,31,'#9c9c94')+pci(454,22,330,3,31,'#9c9c94')
 for row in range(2):
  for col in range(2):s+=drive(824+col*155,23+row*45,147,39,'rear','#78917f')
 s+=psu(822,131,151,87,'#ec9b49','1100W')+psu(984,131,151,87,'#ec9b49','1100W')
 s+=ports(91,182,4)+circle(310,194,11,'#6487bb','#b4c0c8')+ports(365,182,1)+use('usb',456,181)+use('usb',456,201)+use('vga',542,184)
 s+=rect(653,181,69,18,'#263641','#87929b',1)
 return s

def rear_huawei(m):
 s=frame(240,True)+pci(93,23,332,3,31,'#7c929b')+pci(460,23,313,3,31,'#7c929b')
 s+=rect(823,22,306,81,'url(#rear-hole)','#7d909b',1)
 s+=psu(822,125,151,93,'#b4c556','750W')+psu(985,125,150,93,'#b4c556','750W')
 s+=use('usb',82,182)+use('usb',82,201)+use('usb',121,182)+use('vga',203,189)
 s+=rect(309,178,169,35,'#3e8a79','#1d564a',1)+ports(314,183,4)
 s+=use('usb',512,182)+use('usb',512,202)+rect(589,181,169,31,'url(#rear-hole)',rx=1)
 return s

def rear_inspur(m):
 s=frame(240,True)+pci(92,22,307,3,30,'#a4aead')+pci(414,22,301,3,30,'#a4aead')+pci(733,22,390,2,29,'#98acb8')
 s+=rect(987,97,137,34,'url(#rear-hole)','#85969f',1)
 s+=psu(823,136,151,81,'#69a9cd','800W')+psu(986,136,149,81,'#69a9cd','800W')
 s+=ports(90,182,4)+circle(316,195,11,'#5e86b0','#a4b0ba')+ports(372,182,1)+use('usb',469,181)+use('usb',469,201)+use('vga',691,186)
 s+=rect(566,179,63,36,'url(#rear-hole)',rx=1)+rect(648,179,27,34,'#ca7754',rx=1)
 return s

BUILDERS={
 'hpe':(front_hpe,rear_hpe),'dell':(front_dell,rear_dell),
 'lenovo':(front_lenovo,rear_lenovo),'huawei':(front_huawei,rear_huawei),'inspur':(front_inspur,rear_inspur)}

def make_panel(m,brand,side):
 h=m['units']*120
 content=BUILDERS[brand][side=='rear'](m)
 return f'<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="{h}" viewBox="0 0 1200 {h}" preserveAspectRatio="none" role="img" aria-label="{escape(m["vendor"]+" "+m["model"]+" "+side)}"><title>{escape(m["model"])} / {side} / vector review</title>{defs(brand)}{content}</svg>'

def embedded_panel(m,side,x,y,w):
 r=ET.parse(OUT/m['id']/(side+'.svg')).getroot()
 # Unique IDs avoid cross-model SVG definitions accidentally sharing paint.
 ids={e.attrib['id']:m['id']+'-'+side+'-'+e.attrib['id'] for e in r.iter() if 'id' in e.attrib}
 for e in r.iter():
  for k,v in list(e.attrib.items()):
   if k=='id':e.set(k,ids[v]);continue
   for a,b in ids.items():v=v.replace('url(#'+a+')','url(#'+b+')')
   if v.startswith('#') and v[1:] in ids:v='#'+ids[v[1:]]
   e.set(k,v)
 r.set('x',str(x));r.set('y',str(y));r.set('width',str(w));r.set('height',str(w*m['units']/10))
 return ET.tostring(r,encoding='unicode')

def sheet(items,name,title):
 width=1900;height=170+len(items)*330+86
 s=f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}">'+rect(0,0,width,height,'#f1f4f8',rx=0)
 s+=text(64,68,title,33,'#1e2d3d','700')+text(65,110,'统一矢量语言 · 保留产品原色、品牌标识和面板特征 · 待确认，未发布',21,'#627386')
 s+=text(84,160,'正面 / FRONT',14,'#6d7e90','600')+text(1000,160,'背面 / REAR',14,'#6d7e90','600')
 for i,m in enumerate(items):
  y=180+i*330;s+=rect(56,y,1788,300,'#ffffff','#d9e1e9',14)
  s+=text(82,y+40,f'{m["vendor"]}  {m["model"]}',24,'#253747','600')+text(1745,y+40,str(m['units'])+'U',20,'#6e8091','600')
  w=838;hh=w*m['units']/10;top=y+77+(170-hh)/2
  s+=embedded_panel(m,'front',82,top,w)+embedded_panel(m,'rear',998,top,w)
  description=NOTES[m['id']]
  s+=text(82,y+279,description,16,'#62758a')
 s+=text(64,height-30,'素材按参考配置绘制；接口灯使用中性色，不表示在线状态。品牌标识为矢量轮廓。',16,'#7d8a98')+'</svg>'
 (OUT/(name+'.svg')).write_text(s)

NOTES={
'hpe-dl380-gen10':'黑色密孔面罩、银色肩线、HPE 绿色标识；背面保留紫色电源卡扣。',
'hpe-dl360-gen10':'1U 窄面罩保留 HPE 银色轮廓与绿色标识，背面呈现紧凑扩展区和双电源。',
'hpe-dl380-gen11':'保留 Gen11 银色交叉骨架、纵向格栅与绿色 HPE 标识，区别于 Gen10 密孔面罩。',
'dell-r740':'按确认的整机图重绘：银灰色大蜂窝面罩、DELL EMC、左侧锁孔与右侧竖向接口区。',
'dell-r750':'保留 Dell 15G 标志性双排银色几何面罩与 DELL EMC 字样，背面保留橙色电源卡扣。',
'dell-r650':'保留 1U 低矮几何面罩与 DELL EMC 字样，双电源分布在背面两侧。',
'lenovo-sr650-v2':'保留黑色竖向盘架、右侧蜂窝空位与白色 Lenovo 标识；背面双电源保留橙色卡扣。',
'huawei-2288h-v5':'保留深色盘架、左侧华为花瓣标识、蓝色接口与右侧蜂窝面板；背面电源卡扣保留黄绿色。',
'inspur-nf5280m6':'保留蓝灰金属盘托、横向 12 盘位与浪潮信息品牌标识；背面保留彩色接口和蓝色电源卡扣。',
'h3c-s5130s-28s-ei':'已修正网口卡扣方向：上排朝上、下排朝下；保留 H3C Logo。'}

def main():
 for m in CATALOG:
  if m['id'].startswith('h3c-'):continue
  brand=m['id'].split('-')[0];folder=OUT/m['id'];folder.mkdir(exist_ok=True)
  for side in ['front','rear']:(folder/(side+'.svg')).write_text(make_panel(m,brand,side))
  references={side:{k:m[side][k] for k in ['sourcePage','imageUrl','sha256','original']} for side in ['front','rear']}
  if m['id']=='dell-r740':
   approved=json.loads((folder/'reference-front-bezel.json').read_text())
   references['front']={k:approved[k] for k in ['sourcePage','imageUrl','sha256']}
   references['front']['original']='vector-draft/dell-r740/reference-front-bezel.jpg'
  (folder/'reference.json').write_text(json.dumps({'id':m['id'],'model':m['model'],'units':m['units'],'status':'review-only','appearance':NOTES[m['id']], 'reference':references},ensure_ascii=False,indent=2))
 sheet(CATALOG[:3],'review-hpe','HPE / 绿色标识与银黑面罩')
 sheet(CATALOG[3:4],'review-r740','Dell PowerEdge R740 / 已按确认参考图重绘')
 sheet(CATALOG[3:6],'review-dell','Dell / 几何面罩与银黑机身')
 sheet(CATALOG[6:9],'review-other','Lenovo · Huawei · Inspur / 各自保留产品外观')
 sheet(CATALOG,'review-all','10 款设备矢量素材 / 正背面总览')
 cards=[]
 for m in CATALOG:
  cards.append(f'<article><h2>{escape(m["vendor"]+" "+m["model"])} <small>{m["units"]}U</small></h2><div class="panels"><figure><img src="{m["id"]}/front.svg"/><figcaption>正面 · <a href="{m["id"]}/front.svg">打开 SVG</a></figcaption></figure><figure><img src="{m["id"]}/rear.svg"/><figcaption>背面 · <a href="{m["id"]}/rear.svg">打开 SVG</a></figcaption></figure></div><p>{NOTES[m["id"]]}</p></article>')
 (OUT/'index.html').write_text('<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>设备矢量素材 · 待评审</title><style>body{margin:0;padding:36px;background:#f1f4f8;color:#253747;font:16px system-ui}main{max-width:1700px;margin:auto}header{margin-bottom:30px}article{background:white;border:1px solid #d9e1e9;border-radius:14px;padding:24px;margin:18px 0}h2{font-size:21px}small,p,figcaption{color:#62758a}figure{margin:0;flex:1;min-width:0}img{width:100%;display:block;min-height:90px;object-fit:contain}figcaption{margin-top:12px}a{color:#6650a0}.panels{display:flex;align-items:center;gap:32px}@media(max-width:900px){.panels{display:block}figure{margin-bottom:20px}}</style><main><header><h1>设备矢量素材 · 待评审</h1><p>统一视角、细节和光照；保留产品自身颜色、品牌和布局。本目录尚未替换项目当前素材。</p><p><a href="review-all.svg">打开完整总览</a></p></header>'+''.join(cards)+'</main></html>')
 print('Built 18 new SVG panels, plus 5 review sheets and an offline gallery. Published catalog unchanged.')
if __name__=='__main__':main()
