---
name: Bug Report
about: Report a bug or unexpected behavior in DXClusterGoAPI
title: ''
labels: bug
assignees: ''
---

## Description

A clear and concise description of the bug.

## Steps to Reproduce

1.
2.
3.

## Expected Behavior

What you expected to happen.

## Actual Behavior

What actually happened.

## Environment

- **Go version** (if building from source):
- **Deployment method**: <!-- Docker / binary / source -->
- **OS / architecture**:
- **DXClusterGoAPI version / commit**:

## Configuration

Relevant environment variables (redact any credentials):

```
# Example — redact passwords and sensitive values
CLUSTERS=dx.example.com:7300,cluster2.example.com:7300
LOG_LEVEL=info
POTA_ENABLED=true
REDIS_ADDR=
LOTW_ENABLED=false
```

- Number of configured clusters:
- POTA integration enabled: <!-- yes / no -->
- LoTW integration enabled: <!-- yes / no -->
- Redis/caching enabled: <!-- yes / no -->

## Logs

Relevant log output. If possible, reproduce with `LOG_LEVEL=debug`:

```
paste logs here
```

## Additional Context

Any other information that may help diagnose the issue (e.g. network topology, firewall rules, cluster software version).
