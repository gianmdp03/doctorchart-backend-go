"""Build and test the actual frontend using only explicit public fixture URLs."""
import os, pathlib, subprocess
ROOT = pathlib.Path(__file__).resolve().parents[1]
FRONT = ROOT.parent / 'doctorchart-frontend'
ENV = dict(os.environ, PUBLIC_APP_URL='http://localhost:4202', PUBLIC_API_URL='http://localhost:18080/api/doctorchart',
    PUBLIC_AI_GATEWAY_URL='http://localhost:18080/api/ai/doctorchart/chat', PUBLIC_SYNTAH_PLANS_URL='http://localhost:18999/fixture-plans',
    PUBLIC_SUPABASE_URL='https://localhost:19443', PUBLIC_SUPABASE_PUBLISHABLE_KEY='sb_publishable_doctorchart_local_fixture')
commands = [('tests', ['npm.cmd','test','--','--watch=false']), ('production-build', ['npm.cmd', 'run', 'build', '--', '--configuration', 'production']),
    ('typecheck', ['node', 'node_modules/typescript/bin/tsc', '-p', 'tsconfig.app.json', '--noEmit']),
    ('public-config', ['npm.cmd', 'run', 'test:config']), ('production-ssr', ['npm.cmd', 'run', 'test:ssr'])]
failed = False
for name, command in commands:
    with (ROOT / 'integration/evidence' / ('frontend-' + name + '.log')).open('w', encoding='utf8') as out:
        result = subprocess.run(command, cwd=FRONT, env=ENV, stdout=out, stderr=subprocess.STDOUT)
    print(name, 'PASS' if result.returncode == 0 else 'FAIL', flush=True)
    failed |= result.returncode != 0
raise SystemExit(1 if failed else 0)
