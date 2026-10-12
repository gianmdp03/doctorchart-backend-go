# go-security-audit

A Codex-compatible, repo-independent security audit skill for Go services, PostgreSQL and deployment infrastructure. **No backend repositories are included or modified.**

## Installation into a Codex project

Copy the `go-security-audit` directory to `<your-repository>/.agents/skills/go-security-audit/`, or to your supported personal skills location if you want to share it across projects. The `.agents/skills/` location is a project-local convention; verify your Codex version's skill discovery configuration.

In Codex ask: `Use go-security-audit in plan mode on this repository. Do not modify code or install tools yet.`

## Optional helper commands

```bash
python scripts/audit.py preflight --repo /path/to/go-repo
python scripts/audit.py run-static --repo /path/to/go-repo --out /path/outside/repo/security-report
python scripts/audit.py cleanup --manifest /path/outside/repo/security-report/run-manifest.json
```

On Windows replace `python` with `py -3` and use Windows paths. The static runner uses only already-installed tools. Add `--allow-network` to run network/cache-dependent scanners **only after approving network access**. The runner does not run ZAP, active attacks, database writes, `go test`, installs, fixes, or deployment. The agent must separately review test safety, arrange isolated verified tool installations after approval, and produce the full audit report. Report output may contain sensitive scanner snippets; secure it and redact before sharing.

## Limits

Security assessment requires human judgment, authorization and appropriate test fixtures. Passing scanners does not guarantee security. Do not execute scans against production or third-party hosts without authorization. The included helper creates only a temporary run directory and output directory, and cleanup only removes the owned temp directory.
