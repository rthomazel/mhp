# MHP contributor guidelines

This repository is in design review. `doc/plan.md` is the requested single review artifact; do not implement until the operator approves the architecture and resolves relevant open decisions.

- One Go binary with relay, exit, and proxy modes. Prefer standard library code and small reviewed dependencies.
- Preserve authenticated, verified TLS and loopback-only local proxy defaults. Never log secrets or payloads.
- Bound network setup, buffering, retries and goroutine lifetimes; retry forever must remain cancellable.
- Reuse netdiag selectively, with provenance and permission; do not inherit its diagnostic security exceptions.
- Keep documentation synchronized with approved decisions. Mark proposals explicitly rather than claiming approval.
- Use Markdown with lightweight OKF frontmatter for design artifacts. For this initial review, keep architecture and questions in `doc/plan.md` as requested rather than duplicating them under `spc/`.
- Future Go changes should be formatted, vetted, tested (including race checks where appropriate), and cross-built for Windows without cgo.
- Use signed commits and pull requests; never push directly to main.
