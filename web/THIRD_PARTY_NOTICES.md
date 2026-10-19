# Third-party notices and release packaging

The browser distribution ships its complete notices at [`public/third-party-notices.txt`](public/third-party-notices.txt), served at `/third-party-notices.txt`. It includes React 19.2.5, React DOM 19.2.5, their runtime dependency scheduler 0.27.0, and the fixed infinite-canvas source below. Each included license/notice is copied in full from the installed package or the retained upstream canvas license, with its SHA-256 recorded.

`npm run build` first runs `scripts/build-notices.mjs`. The generator follows the installed production dependency graph, verifies package names/versions and locations against `package-lock.json`, and refuses missing, empty, unreadable or non-file licenses/notices. It does not infer license text from an SPDX label. `node scripts/build-notices.mjs --check` verifies the generated file against the current installed inputs; Vite copies that public file into `dist`, and CI checks that its bytes are unchanged. Build-only development dependencies are not part of this browser runtime notice list.

The canvas interaction layer derives from [basketikun/infinite-canvas](https://github.com/basketikun/infinite-canvas), fixed commit `3a6b64b602ad062b29fac66902ed18a7ef5fd9dc` (v0.20.0).

Copyright (c) 2026 basketikun. MIT License. The complete license also ships separately at `public/infinite-canvas-LICENSE.txt` and is linked from the application's account page.

Reused files:

- `src/components/canvas/infinite-canvas.tsx`: upstream pointer, pan, wheel zoom, drop and background-grid component.
- `src/components/canvas/canvas-connections.tsx`: upstream Bezier connection rendering.
- `src/lib/canvas/canvas-node-geometry.ts`: upstream grouping, bounds, anchor, and focus algorithms.
- `src/components/canvas/canvas-create-menus.tsx`: adapted upstream node and connection creation menus, including outside-click dismissal and inverse-scale placement. Creation uses platform node types and reference validation.
- `src/components/canvas/canvas-node-placeholder.tsx`: adapted empty-media state from upstream `canvas-node.tsx`. Creating an image/video node leaves a placeholder until the user opens its composer.
- `src/components/canvas/canvas-node-hover-toolbar.tsx`: adapted screen-positioned node actions. Only platform preview, download, task, edit, copy, delete and ungroup actions are retained.
- `src/components/canvas/canvas-node-prompt-panel.tsx`: adapted upstream node-local reference/prompt/model/parameter composition and gesture isolation. Platform products, quotes and explicit task confirmation replace personal provider configuration and direct execution.
- `src/components/canvas/canvas-node-reference-bar.tsx`: adapted upstream visual reference strip, add/pick interaction and per-reference controls. Platform asset IDs replace local media storage, with explicit reference/start/end roles.
- `src/components/canvas/canvas-appearance.tsx`: appearance controls follow the upstream toolbar, adapted to platform theme preferences and project-saved background/image-information settings.

The node shell, selection/history integration and cloud persistence remain platform adapters; these adaptations do not import the full upstream application or its stores. Platform-specific replacements include the type subset and a reactive light/dark theme preference store. Personal theme preferences use browser storage; background style and image-information visibility use the platform's project document and history. The upstream application's provider clients, user-configurable API credentials, scripts, plugins, local agents, WebDAV, IndexedDB project persistence, example assets and sponsorship content are not included.

The original author's name and the infinite-canvas project remain visible in the canvas footer and account page. Upstream README additionally requests retaining author information and a frontend identifier; these visible credits are retained alongside the MIT notice.

## Go binaries in the container image

After building `/studio` and `/monitor`, the backend build stage runs `go run ./cmd/licenses -out /licenses -binary /studio -binary /monitor`. The collector reads dependency modules from each binary's embedded Go build information, rather than treating all development/test modules or the collector's own packages as release dependencies. It verifies each module's version and checksum against `go list -m -mod=readonly -json all`, then recursively copies its actual `LICENSE`/`LICENCE`/`COPYING`, `NOTICE` and supplementary `AUTHORS`/`COPYRIGHT`/`PATENTS` files while preserving module-relative paths. This includes the separately licensed `internal/sync/singleflight` code used by AWS SDK and Smithy. A module-root license remains required; nested notices cannot substitute for it. Missing licenses, empty/unreadable notices, any module symlink, module replacements, unavailable/mismatched sources and a mismatched Go toolchain version fail the build; there are no silent exceptions.

The final image includes `/app/licenses/manifest.json`, per-module original files, and `/app/licenses/go/LICENSE` from the exact Go toolchain version used for both binaries. The manifest records binary hashes, package names, toolchain versions, module versions/checksums and notice hashes. Collection only accepts a new/empty output directory and does not delete or overwrite existing files. Debian package copyright/license materials remain supplied by the installed packages under `/usr/share/doc`; these are separate from the Go module inventory.

Recognized legal-document filenames have no extension or a `.txt`, `.md` or `.rst` extension; source/configuration files such as `notice_response.go` and `license-check.yml` are excluded. Dependency updates still require reviewing their actual license layout and the resulting inventory. The current linked Go modules include two nested BSD licenses for AWS SDK/Smithy singleflight, in addition to the module-root notices.

The isolated container smoke checks the public browser notices and verifies the shipped Go binaries/notice files against the packaged manifest. This source workflow does not itself prove a Docker image or production deployment has been built or tested.
