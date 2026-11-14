# Jellyfin Hue Sync

Server-side ambient lighting for **Philips Hue Entertainment** driven by your
**Jellyfin** playback — without screen capture and without a Jellyfin plugin.

It is a self-contained Go web app that runs in its own Docker container next to
Jellyfin. The DTLS 1.2 PSK transport and Hue Entertainment 2.0 packet format are
a Go port of the proven implementation in the
[HueSync Android TV](https://github.com/snapps91/huesync) project, using
[Pion DTLS](https://github.com/pion/dtls) for the PSK stream.

## How it works

Jellyfin does not expose decoded video frames. It only exposes a *playback
clock* (the Sessions API). So the pipeline is:

1. **Playback clock** — poll `GET /Sessions`, select the configured
   `DeviceId`/`UserId` (or the first playing session), and read
   `PositionTicks`, `IsPaused`, and the media file `Path`.
2. **Frame extraction** — a separate `ffmpeg` process decodes the *original
   library file* (not Jellyfin's stream) into low-resolution RGB frames:
   `96×54` at `4 fps`, with HDR tone-mapping when the source is HDR.
3. **Position → frame mapping** — `index = (positionTicks/10⁷ − segmentStart) × fps`,
   cached per 30-second segment with the next segment prefetched.
4. **Color** — each Hue channel's screen position samples a neighborhood of the
   frame, applies brightness/saturation in linear light, encodes to sRGB, and
   smooths over time.
5. **Send to Hue** — the DTLS 1.2 PSK stream carries the colors to the Hue
   Entertainment area (same transport as the Android TV app).

Everything stays on your local network: verified Hue HTTPS for the API and
DTLS for the stream. No cloud, no frames stored.

## Requirements

- A Philips Hue Bridge with an Entertainment area created in the Hue app.
- A Jellyfin server (any install; the Docker workflow below uses the
  `linuxserver/jellyfin` image).
- Read access to the **original media files** from this container. This means
  locally stored movies/TV only — not DRM, live TV, or remote streams.
- `ffmpeg` (included in the provided image).

## Quick start

```bash
cd JellyfinHueSync
# 1. Edit docker-compose.yml: point the media mount at the SAME files
#    Jellyfin serves (read-only).
# 2. Build and start.
docker compose up -d --build
```

Open `http://<host>:8091`.

The container uses **host networking** so Bridge discovery (subnet scan), the
DTLS UDP stream to the Bridge on port 2100, and access to Jellyfin all behave
exactly as they would on the host.

### Example: Unraid with Jellyfin on the same host

If Unraid and Jellyfin both live at `192.168.188.100` and Jellyfin listens on
`8096`, then in the web UI enter:

- **Jellyfin URL:** `http://192.168.188.100:8096`
- **Web UI:** `http://192.168.188.100:8091`

## Prebuilt image (GitHub Actions)

On every push to `main`, every `v*` tag, and manual dispatch,
`.github/workflows/docker.yml` runs `go vet` + `go test`, then builds the image
and pushes it to the GitHub Container Registry and Docker Hub:

```text
ghcr.io/<owner>/<repo>:latest
docker.io/<dockerhub-user>/jellyfinhuesync:latest
```

Tags are produced for the branch name, `latest` (default branch only), a short
commit SHA, and semver tags for `v*` releases (`v1.2.3` → `1.2.3`, `1.2`).
Replace `<owner>/<repo>` with your GitHub repository path (lowercase).

### Pulling on Unraid

GitHub's registry requires authentication even for public packages, so do this
once:

1. Create a GitHub **personal access token** with the `read:packages` scope.
2. In Unraid: **Settings → Docker**, and add a registry entry:
   - **Registry:** `https://ghcr.io`
   - **Username:** your GitHub username
   - **Password:** the PAT
3. Add a container with repository `ghcr.io/<owner>/<repo>:latest`, or pull
   from the Unraid terminal:

```bash
docker login ghcr.io -u <github-username>   # paste a read:packages PAT
docker pull ghcr.io/<owner>/<repo>:latest
```

> The workflow builds `linux/amd64` (what Unraid runs). Add `linux/arm64` to
> `platforms` in `docker.yml` if you ever need it.

### Building locally instead

```bash
docker build -t jellyfin-hue-sync .
```

## Wiring it to your linuxserver Jellyfin

The simplest setup is to mount the **same host directory at the same container
path** in both Jellyfin and this container, so Jellyfin's reported media paths
resolve identically here — no media path mapping needed.

Example — your Jellyfin binds the media share like this:

```yaml
# jellyfin compose (for reference — not this project)
services:
  jellyfin:
    image: lscr.io/linuxserver/jellyfin:latest
    volumes:
      - /mnt/user/appdata/jellyfin:/config
      - /mnt/user/appdata/jellyfin/jellyfin_cache:/cache
      - type: bind
        source: /mnt/user/media
        target: /media
```

then mount the same share at the same target here:

```yaml
# this project's docker-compose.yml
services:
  jellyfin-hue-sync:
    volumes:
      - /mnt/user/media:/media:ro
```

Because both containers now see the files under `/media/...`, a path Jellyfin
reports (e.g. `/media/movies/Movie.mkv`) already exists in this container —
leave the web UI media mappings empty.

If your two containers mount media at *different* paths, use the media path
mappings instead. For example, if Jellyfin sees `/data/media/movies` but this
container mounts the same files at `/media/movies`, add:

```
/data/media/movies -> /media/movies
/data/media/tv -> /media/tv
```

## Web UI walkthrough

1. **Hue Bridge** — *Search network* (scans your subnet for a Bridge) or type
   the IP and *Identify*. Then press *Pair Bridge* and push the physical
   button on the Bridge within ~30 seconds.
2. **Entertainment Area** — *Refresh areas* and select one.
3. **Jellyfin** — enter the URL (e.g. `http://192.168.188.100:8096`), an API
   key, optional Device/User ID filters, and the media path mappings. *Test*
   then *Save*.
4. **Light Response** — brightness, saturation, smoothing, and left/right
   inversion (applied on the next start).
5. **Sync** — *Start sync*. The status line and live color swatches show what
   is being sent to each lamp.

## Configuration and storage

- `CONFIG_DIR` (default `/config`) holds `config.json`, which includes the Hue
  pairing secret (`clientkey`). The file is written with `0600` permissions.
- `PORT` (default `8091`) changes the web UI port.
- Because this is a self-hosted LAN service rather than an Android app, the
  pairing secret is stored in that volume file instead of Android Keystore.

## Is `hue_ca.pem` safe to publish?

Yes. `internal/hue/hue_ca.pem` contains only the **public Philips Hue root CA
certificates** used to verify the Bridge's HTTPS certificate. They are trust
anchors, not secrets, and are distributed by Philips/Signify in every Hue
integration. The actual secrets — the `clientkey` pairing secret and the
`username` application key — are stored only in `config.json` (`0600`,
gitignored) and never committed.

## Tuning (advanced)

The persisted options include a few fields not exposed in the UI:

| field           | default | meaning                                   |
|-----------------|---------|-------------------------------------------|
| `extractFps`    | 4       | frames decoded per second                 |
| `streamRate`    | 25      | packets sent per second to the Bridge     |
| `frameWidth`    | 96      | decoded frame width                       |
| `frameHeight`   | 54      | decoded frame height                      |
| `segmentSeconds`| 30      | decoded segment length (current + next)   |

Edit them in `/config/config.json`, or extend the UI. Higher `extractFps` or
frame size costs more server CPU.

## HDR

HDR sources (`smpte2084`/`arib-std-b67` transfer) are tone-mapped to SDR via
ffmpeg's `zscale` + `tonemap` filters. The app probes ffmpeg at startup and
falls back to a plain scale when `zscale` is unavailable (HDR will look washed
out in that case). Install an ffmpeg build with `libzimg` for full HDR
support.

## Caveats

- Sync follows `PositionTicks` (~1 second resolution), not frame-perfect
  timing — fine for ambient lighting, not for per-frame effects.
- Decoding runs on CPU (a 96×54, 4 fps decode is cheap). The `jellyfin-amd`
  Docker mod accelerates Jellyfin's own transcoding; it does not accelerate
  this container's ffmpeg.
- Two or more simultaneous Hue Entertainment streamers cannot own the same
  area; this app stops if another Hue app takes the area, and vice versa.

## Development

Needs Go 1.25+.

```bash
# Build
go build ./...

# Run tests
go test ./...

# Vet
go vet ./...

# Run
CONFIG_DIR=./config PORT=8091 go run ./cmd/jellyfin-hue-sync
```

`CONFIG_DIR` and `PORT` environment variables control storage and the HTTP
port. The UI is a Vue app served from `internal/web/` (embedded into the binary
via `//go:embed`), using the vendored global Vue build — no Node toolchain
required. The HTTP API is under `/api/*`.

### Layout

```
cmd/jellyfin-hue-sync/   entrypoint
internal/app/            HTTP routes, DTOs, application facade
internal/config/         config.json persistence (0600, atomic writes)
internal/hue/            Hue HTTPS API, DTLS-PSK stream, packet, discovery, CA
internal/sync/           ffmpeg decode, segments, color math, smoothing, controller
internal/web/            embedded Vue web UI (vendored vue.global.prod.js)
```
