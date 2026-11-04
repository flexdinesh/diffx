Diffx Native Experimental

This archive contains a Go-only MyGo UI and its private matching Diffx CLI.
Do not replace either executable independently. The private diffx is not a
PATH installation and must not replace an existing user's diffx command.

Install with install-native.sh to verify SHA-256 and atomically select a pair.
macOS: the .app bundle can also be extracted into a user-owned Applications
directory. Linux: keep the diffx-native directory together; put its executable
on PATH before registering diffx-native.desktop with your desktop environment.
Desktop launches use --desktop-launch to attach or choose a folder. Shell
launches review the current directory or explicit local arguments.

No Developer ID signing, notarization, DMG, auto-update or Windows support.
Native rendering uses system platform libraries, not a webview. The private
server retains embedded web assets for normal browser coexistence. Its web
dependencies and licensing exceptions are described in docs/release.md.

Stop running apps before uninstalling. Private version directories are retained
across upgrades so older running apps keep their matching sidecar. Remove
~/.local/bin/diffx-native and ~/.local/bin/.diffx-native-versions to uninstall
the default install. Review data and preferences are not removed.
