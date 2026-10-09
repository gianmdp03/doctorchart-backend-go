"""One-time extraction of literal catalog data; not a Java implementation translator."""
import json
import re
from pathlib import Path
root=Path(__file__).resolve().parents[1]
source=root.parent/'doctorchart-backend/src/main/java/com/syntah/doctorchart/service/MedicationCatalogService.java'
keys=['name','genericName','presentation','defaultDose','administrationRoute','typicalFrequency','typicalDurationDays','category']
catalog=[]
for index,match in enumerate(re.finditer(r'new MedicationDTO\(id\+\+, (.*?)\)\);',source.read_text(encoding='utf-8')),1):
    values=json.loads('['+match[1]+']')
    if len(values)!=len(keys):raise ValueError('Unexpected catalog entry')
    catalog.append(dict(id=index,**dict(zip(keys,values))))
if len(catalog)!=25:raise ValueError('Unexpected catalog size')
(root/'internal/clinical/medications.json').write_text(json.dumps(catalog,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
print('Extracted 25 literal medications')
