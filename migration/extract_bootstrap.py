"""Export only immutable startup definitions from the owned pinned-Spring fixture."""
import json
from fixture import ROOT, WORK, sql

cfg=json.loads((WORK/'fixture.json').read_text())
if not cfg['container'].startswith('doctorchart-go-fixture-'):raise RuntimeError('Unowned fixture')
result={}
for table in ['specialties','wizard_defaults','chart_definitions','clinical_calculation_definitions','specialty_calculator_defaults','vaccination_calendars','vaccination_doses']:
    result[table]=json.loads(sql(cfg,'spring',f"SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM {table} t;").strip())
for row in result['specialties']:
    for key in ['created_at','updated_at']:row.pop(key)
for row in result['wizard_defaults']:row.pop('updated_at')
directory=ROOT/'internal/bootstrap';directory.mkdir(exist_ok=True)
(directory/'seed.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
print('Exported immutable definitions only; no patient/user data or credentials.')
