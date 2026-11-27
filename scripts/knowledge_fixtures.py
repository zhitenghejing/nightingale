#!/usr/bin/env python3
"""Generate disposable archive fixtures without Office or PDF dependencies."""
import argparse
from pathlib import Path
import zipfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output', type=Path)
    root = parser.parse_args().output
    root.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(root / 'interface-guide.docx', 'w') as z:
        z.writestr('[Content_Types].xml', '''<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>''')
        z.writestr('_rels/.rels', '''<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>''')
        z.writestr('word/document.xml', '''<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>清禾检验接口培训说明</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>报告回传检查窗口</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>25 分钟</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p><w:r><w:t>这是隔离测试资料；不得直接删除业务数据。只有经过业务核对才能确认恢复。</w:t></w:r></w:p><w:sectPr/></w:body></w:document>''')
    stream = b'BT /F1 14 Tf 50 760 Td (ALPHA-KB-PDF maintenance guide) Tj 0 -30 Td (Report verification window is 35 minutes.) Tj ET'
    objects = [b'<< /Type /Catalog /Pages 2 0 R >>', b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>', b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>', b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>', b'<< /Length ' + str(len(stream)).encode() + b' >>\nstream\n' + stream + b'\nendstream']
    pdf = b'%PDF-1.4\n'
    offsets = []
    for i, obj in enumerate(objects, 1):
        offsets.append(len(pdf))
        pdf += str(i).encode() + b' 0 obj\n' + obj + b'\nendobj\n'
    xref = len(pdf)
    pdf += b'xref\n0 6\n0000000000 65535 f \n' + b''.join(f'{n:010} 00000 n \n'.encode() for n in offsets) + b'trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n' + str(xref).encode() + b'\n%%EOF\n'
    (root / 'maintenance-guide.pdf').write_bytes(pdf)


if __name__ == '__main__':
    main()
