# Tooling and installation policy

The skill is cross-platform (Windows PowerShell and Linux). Use existing trusted installations first. Tool execution must not modify source; scanners may create caches, so set temporary cache directories.

| Tool | Use | Caveat |
|---|---|---|
| Go toolchain / `go vet` / `go test` / `go test -race` | Baseline and races | Race support depends on platform/compiler; tests can have side effects: inspect tests and DB config before running |
| `govulncheck` | Reachability-aware Go advisory checks | May need network for vulnerability database; use approved network access and temp caches |
| Semgrep | Static rules | Prefer local OSS engine and local rules; no automatic code upload/login |
| Gitleaks | Secret detection | Redact output; git history can contain live credentials; avoid copying secret values into reports |
| Trivy | Dependency/config/container checks | Downloads vulnerability DB; pin release and use temp cache; scan images only if explicitly in scope |
| OWASP ZAP | Dynamic API tests | Requires exact authorized allowlist and safe environment; active scan requires explicit approval |

## Bootstrap workflow

1. Preflight tools and OS without installing anything.
2. List missing tools with intended version, source, checksum/signature validation plan, disk/network needs and exact install scope.
3. Request approval for downloads/network access. Automatic isolated installation is permitted **after** approval of the plan; no global installation without separate permission.
4. Use a unique temp directory owned by this run, verified official releases, a temporary virtualenv, or containers when available. Never pipe downloaded scripts to shell. Do not treat GitHub 'latest' as a pinned version.
5. Record version and checksum in the report; mark unavailable tools NOT TESTED if installation cannot be safely verified.
6. On exit, clean owned temp files and containers; leave preexisting tools and Docker resources untouched.

No generic auto-download script is included because silently resolving unpinned versions or trusting checksums from the same unverified download would defeat supply-chain checks. The agent should create a specific reviewed bootstrap plan per environment.

## Static runner safety

The included `scripts/audit.py` does not invoke `go test` or any active scanner. It can run `go vet`, `govulncheck`, Semgrep, Gitleaks and Trivy when present, after the agent checks project safety and authorizes network/cache behavior. Results may contain sensitive data: restrict report directory access and redact before sharing.
