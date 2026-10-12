#!/usr/bin/env python3
"""Minimal OpenAPI sanity check: valid YAML, required paths present."""
import sys
try:
    import yaml
except ImportError:
    print("pyyaml not installed; skipping strict check")
    sys.exit(0)

path = sys.argv[1] if len(sys.argv) > 1 else "docs/openapi.yaml"
with open(path) as f:
    spec = yaml.safe_load(f)
required = ["/auth/register", "/auth/login", "/me", "/quizzes", "/ai/generations",
            "/quizzes/{quizId}/live-sessions", "/join/{code}", "/health/live", "/version"]
paths = spec.get("paths", {})
missing = [p for p in required if p not in paths]
if missing:
    print("missing paths:", missing)
    sys.exit(1)
print(f"openapi OK: {len(paths)} paths, version {spec.get('info', {}).get('version')}")
