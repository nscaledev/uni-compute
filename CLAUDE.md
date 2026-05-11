# Development Guidelines

## Architectural Specification

All contributors and AI agents must follow the Nscale Cloud Platform Unified Architectural Specification:

**https://raw.githubusercontent.com/nscaledev/uni-specifications/refs/heads/main/SPECIFICATION.md**

## Pre-commit / Pre-push Checklist

The following make commands must pass before committing and pushing changes:

```sh
make license
make validate
make lint
make generate
[[ -z $(git status --porcelain) ]]  # generated code must be checked in
make test-unit
```

## Documentation Maintenance

The package documentation under `pkg/**/README.md` is part of the implementation contract for this
repository, not optional commentary.

- Before making changes, consult the relevant package documentation so code changes stay aligned
  with the documented architecture, invariants, caveats, API conventions, and lifecycle model.
- Use `pkg/README.md` as the ordered knowledge-graph entry point for service internals, then drill
  into the specific package docs it links to.
- When implementation changes alter behaviour, architecture, lifecycle semantics, error handling,
  trust boundaries, or provider/linkage assumptions, update the affected package documentation in
  the same change.
- Keep the documentation link graph coherent:
  - avoid dead links
  - add links for any new meaningful package documentation
  - update higher-level rollups when lower-level package responsibilities move
- If a code change invalidates an existing caveat or TODO in the docs, correct it rather than
  leaving stale guidance behind.
- Treat the top-level `README.md` and `pkg/README.md` as landing pages that should remain
  technically accurate for new starters, external observers, and AI coding assistants.
- When documentation work is intended to improve, correct, or replace higher-level architecture
  prose, treat repository code, runtime behaviour, and reviewer feedback as the source of truth
  rather than normalizing package docs to an existing architecture document.
