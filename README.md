# KaraokeList

KaraokeList is a local web app for indexing paired CD+G/MP3 karaoke files and
browsing the catalog by title, artist, and genre. The projector page renders
CD+G in a browser Canvas and plays the matching MP3; silent clients can follow
the same playback state.

## Run from source

Requires Go 1.26 or newer. The SQLite driver is pure Go; no C compiler or system
SQLite installation is needed.

```bash
go run . \
  -listen :8080 \
  -library /mnt/data/karaoke \
  -admin-token 'choose-a-local-admin-token'
```

The database defaults to the operating system's user config directory under
`KaraokeList/catalog.sqlite`; override it with `-db`.

Open these routes from a browser on the server or local network:

- `/catalog`: search by title, artist, album, or collection.
- `/artists`: artist index, including tracks without an identified artist.
- `/genres`: genre index. MP3 files without genre tags appear under “Sin género”.
- `/admin`: index the configured library, control playback, and clear the active
  queue, dedications, and greetings with the admin token.
- `/greetings`: send standalone greetings and browse recent messages.
- `/projector`: CD+G + MP3 output; enable audio on this page, then use Admin.
- `/lyrics`: silent synchronized view for phones and other clients.

The server generates an admin token at startup if `-admin-token` is omitted and
prints it to the server log. Use an explicit token or `KARAOKE_ADMIN_TOKEN` for
repeatable local configuration. The app serves plain HTTP; use it only on a
trusted LAN until TLS/authentication is added.

## Indexing and metadata

The indexer pairs `.cdg` and `.mp3` files by relative path and filename stem,
case-insensitively. ID3 title, artist, album, and genre tags are preferred when
present. Generic `Track NN` titles and `Unknown` albums are ignored. If tags are
missing, the indexer reads a leading track number and a recognizable
`track - artist - title` filename pattern. It does not infer missing genres or
artists from collection names. Unpaired files are reported and excluded.

A scan replaces the catalog in one SQLite transaction. If a scan fails or finds
no valid pairs, the existing catalog stays intact. Reindexing preserves IDs for
tracks whose CDG paths remain the same; requests retain their original title,
duration, and media paths, so removing or reordering catalog entries cannot
silently change the song attached to a request. Requests whose media files have
actually disappeared are skipped when playback reaches them. Empty CDG files
are reported and excluded.

Visitors choose a display name once per browser and can change it from the
“Cambiar nombre” action; KaraokeList stores it in an HttpOnly cookie. Song
requests and their optional dedications are persisted with the catalog in SQLite.
Standalone greetings have their own table and appear on `/greetings` and in the
projector marquee. The song dedication stays at the top of the projector while
standalone greetings scroll across the bottom. No separate database service is required. The name cookie
identifies that browser, not an IP address; people on the same network can use
separate devices and names.

When a song ends, the server announces the next queued singer and song on the
projector, then starts it automatically. Admin can configure the announcement
wait from 0 to 120 seconds (15 by default), and pause or resume the countdown.

The server caps JSON request bodies at 16 KiB and uses bounded HTTP read/header
timeouts. It omits a write timeout so long-lived Server-Sent Events connections
remain usable; graceful shutdown stops the player clock and closes active HTTP
requests.

## Player display

CD+G graphics are 288×192 pixels (3:2). The projector scales the image to the
largest size that fits the viewport without stretching; the fullscreen button
hides controls and uses the available display area. The browser itself may add
black bars on displays with a different aspect ratio.
