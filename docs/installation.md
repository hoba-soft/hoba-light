# Install

Download a package from this repository's Releases.
Choose `amd64` for Intel/AMD or `arm64` for ARM, including Apple Silicon.
Checksums are in `SHA256SUMS`. Packages are unsigned; macOS packages are not notarized.

## macOS

Open the `.pkg` file. Install a newer package the same way to update.
Commands are installed in `/usr/local/bin`.

To uninstall:

```sh
sudo rm /usr/local/bin/hobalight /usr/local/bin/hl
sudo rm -rf /usr/local/share/doc/hobalight
sudo pkgutil --forget com.hoba-soft.hobalight
```

## Linux

Replace `VERSION` with the release version.

Debian / Ubuntu:

```sh
sudo apt install ./hobalight_VERSION_linux_amd64.deb
```

Remove with `sudo apt remove hoba-light`.

Fedora:

```sh
sudo dnf install ./hobalight_VERSION_linux_amd64.rpm
```

Update with `sudo dnf upgrade ./<package>.rpm`.
Remove with `sudo dnf remove hoba-light`.

## Windows

Open the `.msi` file, then open a new terminal to use `hl`.
Commands are installed in `%ProgramFiles%\hobalight` and added to PATH.
Install a newer MSI to update. Uninstall through Windows Settings.

## Archives

Extract the `.tar.gz` or `.zip` and run `hl` or `hobalight`.
Update by replacing the executables.

Configuration and backups stay in your home directory by default.
Updating or uninstalling leaves them in place.
See [configuration](usage.md#configuration) to change their location.
