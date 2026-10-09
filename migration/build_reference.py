"""Build pinned Spring strictly from manifest-approved files into target-owned work."""
import hashlib
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path
from fixture import ROOT,WORK,JAVA

subprocess.run([sys.executable,str(ROOT/'migration/verify_source.py')],check=True)
source=ROOT.parent/'doctorchart-backend';destination=WORK/'spring'
manifest=json.loads((ROOT/'migration/source-manifest.json').read_text(encoding='utf-8'))
for relative,expected in manifest.items():
    path=Path(relative)
    if path.is_absolute() or '..' in path.parts:raise RuntimeError('Unsafe manifest path')
    original=(source/path).resolve();target=(destination/path).resolve()
    if not original.is_relative_to(source.resolve()) or not target.is_relative_to(destination.resolve()):raise RuntimeError('Path outside owned copy')
    if hashlib.sha256(original.read_bytes()).hexdigest()!=expected:raise RuntimeError('Pinned source changed')
    target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(original,target)
tooling={}
for relative in ['mvnw','mvnw.cmd','.mvn/wrapper/maven-wrapper.properties']:
    content=subprocess.run(['git','-C',str(source),'show','828691433ac1057d4e7f132685884b5c9cb58e39:'+relative],check=True,capture_output=True).stdout
    target=destination/relative;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(content)
    tooling[relative]=hashlib.sha256(content).hexdigest()
(ROOT/'migration/reference-tooling-manifest.json').write_text(json.dumps(tooling,indent=2)+'\n',encoding='utf-8')
environment=dict(os.environ,JAVA_HOME=str(JAVA.parent.parent))
with (WORK/'reference-build.log').open('wb') as log:
    subprocess.run([str(destination/'mvnw.cmd'),'-DskipTests','package'],cwd=destination,env=environment,stdout=log,stderr=subprocess.STDOUT,check=True)
print('Pinned reference built only in .migration/spring; original project unchanged.')
