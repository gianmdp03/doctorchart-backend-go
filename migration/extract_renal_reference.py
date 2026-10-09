"""Persist real pinned-Spring numerical captures for an exact binary regression test."""
import json
from pathlib import Path
root = Path(__file__).resolve().parents[1]
evidence = json.loads((root / 'migration/evidence/compare.json').read_text(encoding='utf-8'))
values = []
for case in evidence['cases']:
    identifier = case['id']
    if identifier == 'calculator-preview-tfge':
        inputs = {'ageYears': 50, 'creatinineMgDl': 1.2, 'gender': 'FEMALE'}
    elif identifier.startswith('calculator-renal-'):
        _, _, age, gender, creatinine = identifier.split('-')
        inputs = {'ageYears': int(age), 'creatinineMgDl': float(creatinine), 'gender': gender}
    else:
        continue
    body = case['spring']['body']
    values.append({'inputs': inputs, 'rawValue': body['rawValue'], 'formattedValue': body['formattedValue'], 'interpretation': body['interpretation']})
target = root / 'internal/clinical/testdata'
target.mkdir(exist_ok=True)
(target / 'renal_reference.json').write_text(json.dumps(values, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
