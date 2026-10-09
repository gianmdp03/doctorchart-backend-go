"""Prove every pinned source/config/fixture file remains byte-identical."""
import hashlib
import json
import subprocess
from pathlib import Path
root=Path(__file__).resolve().parents[1]
source=root.parent/'doctorchart-backend'
manifest=json.loads((root/'migration/source-manifest.json').read_text(encoding='utf-8'))
changed=[]
for relative,expected in manifest.items():
    path=source/relative
    if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest()!=expected:
        changed.append(relative)
status=subprocess.run(['git','-C',str(source),'status','--porcelain'],check=True,capture_output=True,text=True).stdout.strip()
print(json.dumps(dict(files_verified=len(manifest),changed=changed,git_clean=not status)))
if changed or status:raise SystemExit(1)
