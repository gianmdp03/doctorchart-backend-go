"""Copy immutable clinical labels and text templates; never translate executable Java."""
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[1]
source = root.parent / 'doctorchart-backend/src/main/java/com/syntah/doctorchart/service/calculator'
metadata = {}
for path in sorted(source.glob('*Calculator.java')):
    text = path.read_text(encoding='utf-8')
    code = re.search(r'String code\(\) \{ return "([^"]+)";', text)
    if not code:
        continue
    ranges = []
    for value in re.findall(r'new Interpretation\(([^\n]+)\)', text.split('@Override')[0]):
        ranges.append(dict(zip(['code', 'min', 'max', 'label', 'severity', 'clinicalAction'], json.loads('[' + value + ']'))))
    template = re.search(r'String evolution = """\s*\n(.*?)"""', text, re.S).group(1)
    template = '\n'.join(line[16:] for line in template.splitlines())
    metadata[code.group(1)] = {'name': re.search(r'String name\(\) \{ return "([^"]+)";', text).group(1), 'ranges': ranges, 'evolution': template}
target = root / 'internal/clinical/calculators.json'
if target.exists():
    # These immutable bounded coefficients have separate pinned-JVM evidence.
    metadata['tfge']['ageFactors'] = json.loads(target.read_text(encoding='utf-8'))['tfge']['ageFactors']
target.write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
