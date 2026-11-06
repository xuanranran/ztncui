# ztncui Console 1.0.0

The Go + Vue console is the maintained application. Legacy Node UI source and
its old packaging pipeline have been removed from the main branch. Deployment
is manual; source edits never deploy automatically.

## Local preview

Requirements: Go 1.26+, Node.js 24+, npm.

```powershell
cd console/web
npm ci
npm run build
cd ..
go run ./cmd/ztncui --demo --addr 127.0.0.1:3101 --db data/preview.sqlite
```

Open http://127.0.0.1:3101/networks. Preview credentials:

- Username: `admin`
- Password: `Preview123456!`

The preview uses an isolated SQLite database and sample networks. All controls
write only to this database; it does not connect to or change a real controller.
Preview mode can bind only to a loopback address. Deleting this database resets
the sample data; do so only after stopping the preview.

For frontend development, run `npm run dev` in `web/` while the Go preview runs
on port 3101. Vite listens on http://127.0.0.1:5174 and proxies `/api` locally.
Alternatively, pass `--web-dir ui/dist` to serve the latest local frontend build
without rebuilding the Go executable; omit it to use embedded frontend assets.

## Included workflows

- Network creation opens IPv4 assignment; network editing and confirmed deletion.
- Member search, filters, IPv4 numeric sorting, original order, pins, tags,
  manual addition, editing, individual and batch authorization, IP assignment,
  active bridging and disabling automatic assignment.
- Local join-link QR generation, clipboard operations and member CSV export.
- IPv4 private-prefix generator, presets, multiple address pools; IPv6 ULA
  generation, RFC4193 and 6PLANE modes.
- Managed routes, DNS, multicast, broadcast, MTU and private-network access.
- Native controller JSON flow rules and the official rule-language compiler,
  templates, device exemptions, tags and capability matrices.
- A single administrator enforced by the database, Argon2id password storage,
  legacy Argon2i verification, HTTP-only sessions, CSRF and origin checks,
  login throttling, session revocation on password change, and audit history.
- SSE change notifications, one shared controller sampler, bounded member fetches,
  revision caching, heartbeat/reconnect and polling fallback. Unsaved drafts are
  preserved and network settings use optimistic concurrency checks.
- Desktop, tablet, foldable-width and phone layouts; local light/dark/system
  appearance. Dark page background is `#000000`; cards and dialogs are `#242424`.
- Split frontend routes, on-demand QR module, cached gzip static responses,
  immutable hashed assets and bundled Twemoji Mozilla emoji font.

## Data ownership and migration

ZeroTier remains the source of truth for networks, members and network settings.
SQLite stores only console metadata (names, descriptions, tags, pins), the single
administrator, flow-rule editor source/templates, audit history and isolated sample data in preview mode.

The existing application and its data are not modified. Live deployment
can import an **offline copy** of the old `/opt/ztncui/etc` directory:

```sh
./ztncui-next --db /var/lib/ztncui-console/console.sqlite \
  --legacy-dir /path/to/copied-legacy-etc \
  --zt-address http://127.0.0.1:9993 \
  --zt-token-file /path/to/readonly/zerotier-token
```

The importer reads `passwd` and node-persist `storage/*` in a transaction. It
retains `admin` only, preserves its existing hash, imports names/descriptions/pins,
does not overwrite existing metadata, and never writes the input directory.
Successful legacy Argon2i login upgrades the hash in the new database only.
It does not copy, replace or regenerate ZeroTier identities, planet files,
networks, members, address assignments, or controller data.

Live mode never falls back silently to sample data. With no imported admin,
first-run setup creates exactly one `admin` account. No user creation API exists.
Use `CREDENTIALS_DIRECTORY/zerotier-token` with systemd credentials; the local
ZeroTier token stays server-side and is never sent to the browser.

`/api/export` exports management/network JSON for inspection, **not** a complete
ZeroTier identity/controller backup. Protect a complete controller backup
separately before any migration. The console was manually deployed to
`192.168.2.190:3000` on 2026-11-06, with old service/data retained for rollback.
Production login, metadata import, member lists, sampling and service restart
have been verified; restrictive flow-rule forwarding still requires client tests.

## Build and verify

```sh
cd web && npm ci && npm test && npm run build && cd ..
go test ./...
go vet ./...
go build -trimpath -o ztncui-next ./cmd/ztncui
```

The Go executable embeds the built frontend. `ui/dist`, generated executables,
sample databases and browser verification screenshots are gitignored. The old
Node packaging pipeline has been removed. Console artifact packaging is
separate from manual server deployment.

The `Build Console` GitHub Actions workflow tests the frontend and backend,
including the Go race detector, then uploads `ztncui-console-1.0.0-linux-amd64`.
The artifact contains a versioned `.tar.gz` executable bundle and SHA-256 checksum.
It does not install or deploy automatically. Historical 0.8.8 source and packages
remain available through Git history and old Releases for rollback.

## Debian packages

`Build Debian Packages` builds the original release tag for both `amd64` and
`arm64`, verifies the ELF architecture, and tests installation, real administrator
setup, reinstall, removal and purge in isolated Debian 12 containers (ARM64 uses
QEMU). No installation test runs on a production server.

Install ZeroTier One first, then install the package matching `dpkg --print-architecture`:

```sh
sudo apt install ./ztncui-console_1.0.0_amd64.deb
# Or: sudo apt install ./ztncui-console_1.0.0_arm64.deb
```

The package installs `ztncui-console.service`, with configuration in
`/etc/ztncui-console/console.conf` and the existing database location
`/var/lib/ztncui-console/console.sqlite`. Fresh installs listen on
`127.0.0.1:3000`. Use a local browser or SSH tunnel to set up the administrator;
then change `LISTEN_ADDR=0.0.0.0:3000` if LAN access is needed and restart the
service. Do not expose the uninitialized administrator setup publicly.

The separate legacy `ztncui` package is not automatically removed or migrated.
For legacy migration, stop the old UI, back up its data, copy its `etc` directory
offline into a location readable by `ztncui`, and set `LEGACY_DIR` in the new
configuration before starting the console. See the migration section above.
Do not point both UIs at the same listening port. Existing manual systemd units
under `/etc/systemd/system` continue to override the packaged vendor unit.

Package upgrades preserve application data and edited conffiles. Removal/purge
never deletes `/var/lib/ztncui-console`, the service account, or ZeroTier identity
and controller data. First back up the database and ZeroTier directory before
changing installation methods. Requires systemd 247+ and Linux; Debian 12+ is the
tested baseline. These packages do not include a ZeroTier One daemon.

Twemoji font source, attribution and licenses remain in
`web/public/licenses/Twemoji.Mozilla.NOTICE.txt` and
`web/public/licenses/Twemoji.Mozilla.LICENSE.md`. The font itself is bundled from
`web/src/assets/fonts/Twemoji.Mozilla.ttf`.

The on-demand flow-rule editor uses the upstream MPL-2.0 compiler; attribution
and license are bundled in `web/public/licenses/ZeroTier-rule-compiler.*`.
See [UPSTREAM-COVERAGE.md](docs/UPSTREAM-COVERAGE.md) for the source-backed feature
audit, remaining native options and the local-preview verification boundary.

Native policies are managed under each network's Flow Rules view. Definitions
are written to the embedded controller; human-readable labels and enum names
are console metadata. The member policy endpoint requires both `If-Match`
(member policy revision) and `If-Network-Match` (network revision). Definitions
still assigned to members, or tags referenced by rules, cannot be removed.
Template/reset operations preserve native policies. A terminal `drop` prevents
capability evaluation; use an intentional `break` when capabilities should be
considered. Defaults follow the embedded controller's semantics, not a simulated
cloud subscription. Production forwarding behavior still needs client testing.

## Presence and reachability

Online presence and authorization are separate. The embedded member API may omit
`lastSeen`; native observations use peer-path receive times instead. Missing peer
activity is not proof of an offline device, particularly when clients communicate
through a different gateway.

On Linux, absent native activity is supplemented by low-frequency ICMP probes of
authorized members' assigned private IPv4/ULA addresses only. Probes use a bounded
64-job queue, four workers, at most two addresses, and a configurable per-address
deadline (default 1500 ms). Automatic scheduling defaults to once per minute per assignment;
manual requests have a 15-second cooldown. Positive results are fresh for the configured
interval plus 30 seconds and retain their source; ICMP reachability never implies a direct or
relayed ZeroTier path. No reply is inconclusive: route/firewall restrictions may
block ping. A private IP response also does not cryptographically prove node
identity. Changed assignments cannot inherit a prior address's result.

Sampling never waits for probes. No raw-socket privileges or root service user
are required when the OS permits unprivileged ping. Missing ping/permissions show
as unavailable rather than offline. `--disable-probes` disables this supplemental
detection. Demo mode never sends real probes. Lists expose a manual scan command,
and device details expose per-device results and a manual check.

The connection column distinguishes native direct/relay latency from supplemental
`Ping` checks. Ping timings are approximate server-side response durations,
including command overhead, not native ZeroTier path latency. The last successful
address and duration survive an in-flight/failed recheck only within the positive
freshness window, explicitly labelled as a previous result. Compact layouts show
this connection summary underneath the managed IP.

System Settings exposes the automatic interval (10-3600 seconds) and per-IP
timeout (200-10000 ms), with defaults of 60 seconds and 1500 ms. Settings persist
in SQLite under `settings:probes`; `GET`/`PUT /api/settings/probes` require admin
authentication, and updates require CSRF plus the current `If-Match` revision.
Successful updates are audited and apply to new jobs without a restart. In-flight
jobs retain their original timeout. The ping command's own wait time follows the
job deadline instead of imposing a hidden one-second limit.

Member icons are editable console metadata, scoped to network and member ID.
Automatic selection uses name hints and the gateway tag, not a claim of remote
OS detection. Explicit choices override these hints. OpenWrt variants use a router
symbol. Platform brand silhouettes use Font Awesome Free Brands (CC BY 4.0),
bundled locally with attribution/license at `/licenses/FontAwesome-Brands-LICENSE.txt`.
