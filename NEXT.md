# NEXT.md

This file is the authoritative TODO list for AI-assisted work on little-doc-editor: it holds the
list of tasks needed to continue and finish the implementation (v1 scope per SPEC.md), in the
order they should be done.

**Convention: if this file lists a task, there is outstanding directed work to do.** When you
(the AI) pick this up in a fresh session with no other instruction, read this file and work the
first unblocked item top-to-bottom — items are ordered by dependency, not just priority. When you
finish an item, delete it (or move it to a "Done" note in your PR/commit description — don't leave
completed items lying around here) and update anything it unblocks. If a task turns out to be
already done, or the premise is stale, remove it and say why in your summary to the user rather
than silently skipping it. If this file is empty or says so explicitly, there's no standing
directed work — that's not an error, just meet whatever the user asks in the moment.

Background: SPEC.md is the product/technical spec and decisions log. DEVELOPER.md is the local
setup guide. README.md describes what's built and how to deploy it, ARCHITECTURE.md how it fits
together; trust this file for what's *left*.

**Ordering at a glance:** 8 must be done before any real (non-local) deployment, whatever else
is finished. Whenever the UI changes noticeably, re-run `just screenshots` (DEVELOPER.md, "Demo
data and screenshots"), look at the PNGs, and update README.md's features section.

**Caller capabilities are available for UI gating:** pages carry `my_role` + `can_edit`, spaces
carry `my_role` (see `api/openapi.yaml`). Use `frontend/src/utils/permissions.ts`
(`canEditPage`, `roleAtLeast`) to *hide* what the caller can't do, and add Vitest specs under
`src/**/__tests__/` (`npm test`; jsdom + `@vue/test-utils` are set up — see
`components/__tests__/AttachmentPanel.spec.ts`).

---

## 4. Live Yjs collaboration: leftovers

Frontend wiring is done (`frontend/src/editor/`, `useCollabSession`; collab side per
`collab/README.md`). What's left:

- **New nodes/marks** must be added to both `frontend/src/editor/extensions.ts` and
  `collab/src/schema.ts` in the same change.
- Share-link guests can only reach descendants by page id/in-content link (`/share/:token/:pageId`):
  the api has no guest children-listing endpoint, so there is no tree for guests.

## 8. Required before any real deployment (security hardening)

Not features, but the gaps between "works locally" and "safe to expose" (SPEC.md §8):
- **Redact `/share/<token>` and `/invite/<token>` in ingress/gateway access logs** (the frontend nginx
  and api already do).
- **Deploy-time config to review** (all in the chart, `api.*`): `trustedProxies` (set to your
  ingress CIDR or every client looks like the ingress), `rateLimit.*`, `historyCleanupSchedule`.
