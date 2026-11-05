package app

import (
	"fmt"
	"github.com/ebitengine/purego"
)

// WebKitGTK 2.40 implements the Safari 16.4 shared frontend target. Check the
// actual library MyGo will load, rather than the webview's compatibility UA.
func checkRuntime() error {
	for _, name := range []string{"libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37"} {
		lib, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			continue
		}
		defer purego.Dlclose(lib)
		majorSymbol, err := purego.Dlsym(lib, "webkit_get_major_version")
		if err != nil {
			return fmt.Errorf("WebKitGTK version unavailable: %w", err)
		}
		minorSymbol, err := purego.Dlsym(lib, "webkit_get_minor_version")
		if err != nil {
			return fmt.Errorf("WebKitGTK version unavailable: %w", err)
		}
		var major, minor func() uint32
		purego.RegisterFunc(&major, majorSymbol)
		purego.RegisterFunc(&minor, minorSymbol)
		if major() < 2 || major() == 2 && minor() < 40 {
			return fmt.Errorf("Diffx Desktop requires WebKitGTK 2.40+ (Safari 16.4 target); installed %d.%d", major(), minor())
		}
		return nil
	}
	return fmt.Errorf("Diffx Desktop requires WebKitGTK 2.40+; install your distribution's WebKit2GTK 4.1 runtime")
}
