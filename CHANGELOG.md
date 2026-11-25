# Changelog

## [0.4.0](https://github.com/TaliaMarine/little-doc-editor/compare/v0.3.1...v0.4.0) (2026-11-25)


### Features

* finish v1 scope (accounts, live collab, attachments, sharing, history, export, PWA, hardening, docs) ([4fda950](https://github.com/TaliaMarine/little-doc-editor/commit/4fda95044ed534796dfe4ccc9daec337095905d0))

## [0.3.1](https://github.com/TaliaMarine/little-doc-editor/compare/v0.3.0...v0.3.1) (2026-11-23)


### Bug Fixes

* **frontend:** build static assets on the build platform (arm64 emulation hangs) ([4bebe56](https://github.com/TaliaMarine/little-doc-editor/commit/4bebe56140c08559efa6aac434098693a82cc4a6))

## [0.3.0](https://github.com/TaliaMarine/little-doc-editor/compare/v0.2.2...v0.3.0) (2026-11-23)


### Features

* OIDC group-&gt;admin mapping, default member role, profile info ([258e6a3](https://github.com/TaliaMarine/little-doc-editor/commit/258e6a37571752b04777840ebedf4bdfc0b4a0de))


### Bug Fixes

* **api:** redirect OIDC provider errors to the login page ([22401b0](https://github.com/TaliaMarine/little-doc-editor/commit/22401b0f8cc6585a31574682f826991cbd85482d))

## [0.2.2](https://github.com/TaliaMarine/little-doc-editor/compare/v0.2.1...v0.2.2) (2026-11-23)


### Bug Fixes

* **docker:** distroless debian13 for api; drop npm from collab runtime image (bundled CVEs) ([45ed107](https://github.com/TaliaMarine/little-doc-editor/commit/45ed10758a447e5399a1394093ba6d14c5c92c09))

## [0.2.1](https://github.com/TaliaMarine/little-doc-editor/compare/v0.2.0...v0.2.1) (2026-11-23)


### Bug Fixes

* **docker:** apply Alpine security updates in runtime images ([58fc29c](https://github.com/TaliaMarine/little-doc-editor/commit/58fc29c7acbba311fa2914eb3de5bf1836f3a509))

## [0.2.0](https://github.com/TaliaMarine/little-doc-editor/compare/v0.1.0...v0.2.0) (2026-11-23)


### Features

* OIDC public client (PKCE, no secret), SPA SSO login, PVC attachment storage, CI/publish workflows ([c1d8306](https://github.com/TaliaMarine/little-doc-editor/commit/c1d830661dd72a10a619441d93dd9100397ce9b8))


### Bug Fixes

* **collab:** copy lockfile into the runtime image stage ([b44a39b](https://github.com/TaliaMarine/little-doc-editor/commit/b44a39bda566cc2c9cfedec6fa4232cf8c4516c0))
