"""Read-only visual review of app-produced PDFs against the historical reference."""
from pathlib import Path
import pypdfium2 as pdfium
from PIL import Image, ImageDraw
ROOT=Path(__file__).resolve().parents[1]
evidence=ROOT/'integration/evidence'
paths=[ROOT/'.migration/pdf-prescription-metadata-spring.pdf',evidence/'browser-prescription.pdf']
panels=[]
for path in paths:
    pdf=pdfium.PdfDocument(path.read_bytes())
    panels.append(pdf[0].render(scale=1.6).to_pil().convert('RGB'))
sheet=Image.new('RGB',(sum(x.width for x in panels),max(x.height for x in panels)+35),'white')
draw=ImageDraw.Draw(sheet);left=0
for label,panel in zip(['Historical Spring fixture (different synthetic patient)', 'Current Go PDF from actual Angular printing'],panels):
    draw.text((left+15,10),label,fill='black');sheet.paste(panel,(left,35));left+=panel.width
sheet.save(evidence/'pdf-reference-current.png')
print('Rendered historical reference and current Angular-produced PDF for visual review.')
