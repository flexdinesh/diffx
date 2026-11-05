Diffx Desktop Experimental

This archive contains the MyGo webview host, embedded shared React review app
and its private matching Diffx CLI. Keep both executables together. The private
diffx is not a PATH installation and must not replace the user's diffx command.

Install with install-desktop.sh to verify SHA-256 and atomically select a pair.
macOS: extract the .app into a user-owned Applications directory.
Linux: keep the diffx-desktop directory together and put its executable on PATH
before registering diffx-desktop.desktop. Desktop launches use --desktop-launch
to attach or choose a folder. Shell launches retain local PATH/stdin arguments.

Requires modern system WebKit (Safari 16.4 or equivalent). Linux requires
GTK3 and WebKitGTK 4.1 (WebKit 2.40+); macOS requires 13.3+. macOS runtime acceptance requires a Mac; cross-compiling
does not prove runtime support. No Developer ID signing, notarization, DMG,
auto-update or Windows support. Native OS menus and folder dialogs remain.

The shared frontend supplies design tokens, themes and Pierre rendering.
Server comments and reviewed marks are retained; old native preferences are
not migrated. Browser and desktop preferences are separate.

Stop running apps before uninstalling. Private version directories are retained
across upgrades so running apps keep their matching sidecar. Remove the default
~/.local/bin/diffx-desktop and ~/.local/bin/.diffx-desktop-versions to uninstall.
Review data and configuration files are not removed. Former diffx-native
installations can be removed separately after stopping those apps.
