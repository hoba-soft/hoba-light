# Storage design

`Store` uses `storageBackend` in `internal/hobalight/storage_backend.go`.
`filesystemStorage` is the only configured provider. Cloud providers still need
an adapter, coordination, authentication and CLI configuration.

## Format

```text
store.yaml                      store format
objects/<first two hash chars>/<hash>B
objects/<first two hash chars>/<hash>F
objects/<first two hash chars>/<hash>S
sessions/<timestamp>-<short-hash>.yaml
logs/<run-id>.log
trash/<id>-<unique-suffix>.<reason>
```

- `B`: ZIP with one `data` entry. Its ID hashes the original content.
- `F`: YAML file metadata with a blob ID. Its ID hashes path, size and timestamps,
  not the blob reference. Failed files store an error instead of a blob.
- `S`: YAML session with source path, start time and file IDs. Its ID hashes the
  exact YAML bytes. Session references are rebuildable listing caches.

IDs use SHA-256 plus a type suffix. Internal links use full IDs. Stored paths use
`/` and timestamps use UTC. File metadata and session objects are the source of
truth. GC follows every active `S -> F -> B` link, even without session references.

## Backend contract

Keys are slash-separated paths relative to the storage root. `.` is the root.
Directory entries contain basenames; remote providers may use logical prefixes.
All methods must support concurrent calls. Missing data must wrap `fs.ErrNotExist`.

- `Read` loads small metadata. `Open` returns a reader with random access and a
  stable size for ZIP reads. Callers must close it. Remote readers can use range
  requests or a temporary file.
- `Write` consumes the stream before returning and publishes complete data only.
  Safe writes reject links, non-regular targets and unsafe parent paths.
- `Stat` follows links; `Inspect` does not. Providers without links may use the
  same implementation for both. `EnsureNamespace` creates a safe namespace.
- `Scan` lists immediate children, handling pages internally. Propagate errors;
  `errStopStorageScan` means a successful early stop. `Walk` follows
  `io/fs.WalkDirFunc` rules, including errors and `SkipDir`, without following links.
- `Reserve` reserves a unique key using the pattern's `*`. `Move` may copy then
  delete, but must keep the source if the copy fails. `Remove` removes one entry.
- `NewLog` opens a new backup log. `LocalRoot` is only for excluding local storage
  from source scans; remote providers return an empty string.

Use streamed uploads and local temporary files to keep memory use bounded.
The optional `localStorageStaging` interface lets local storage stage files on
its own volume and publish by rename. Callers clean up staging files.

## Coordination

`BeginWrite` allows concurrent backups/refreshes but excludes maintenance.
`BeginMaintenance` excludes writers and other maintenance until released.
Failed acquisition must clean up its own locks. `Activity` reports both states.
Providers must meet these guarantees or refuse the operation; marker objects
alone are not a safe lock for every remote service.

Local storage uses `.backup-*` files and a `.gc.lock` directory. Crash recovery
is manual; see [usage](usage.md#check-and-clean-up).

Provider SDKs, retries, pagination and upload sessions belong in the adapter.
Source scans, restore destinations and comparison scratch files stay local.
`storage_backend_test.go` exercises storage operations with an in-memory backend;
integration tests cover the filesystem and real CLI.
