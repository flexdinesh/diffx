//go:build desktopautomation && !mygo_noinspector && linux && (amd64 || arm64)

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

// This bridge observes and navigates the real GTK dialog. Only X11 input may
// accept it; no response signal, callback or launcher operation is exposed.
type desktopTest struct{ desktop *Desktop }

func bindDesktopTest(d *Desktop) { mygo.BindAs("DesktopTest", &desktopTest{desktop: d}) }

type pickerBounds struct{ X, Y, Width, Height int32 }
type pickerState struct {
	Visible       bool
	Title         string
	Directory     string
	Filename      string
	Action        int32
	WindowID      uint64
	OpenLabel     string
	OpenResponse  int32
	OpenSensitive bool
	OpenMapped    bool
	OpenBounds    pickerBounds
}

type pickerGTK struct {
	toplevels     func() uintptr
	listLength    func(uintptr) uint32
	listData      func(uintptr, uint32) uintptr
	listFree      func(uintptr)
	chooserType   func() uintptr
	dialogType    func() uintptr
	buttonType    func() uintptr
	isType        func(uintptr, uintptr) int32
	mapped        func(uintptr) int32
	title         func(uintptr) string
	directory     func(uintptr) *byte
	filename      func(uintptr) *byte
	setDirectory  func(uintptr, string) int32
	unselectAll   func(uintptr)
	action        func(uintptr) int32
	defaultWidget func(uintptr) uintptr
	response      func(uintptr, uintptr) int32
	buttonLabel   func(uintptr) string
	sensitive     func(uintptr) int32
	width         func(uintptr) int32
	height        func(uintptr) int32
	translate     func(uintptr, uintptr, int32, int32, *int32, *int32) int32
	window        func(uintptr) uintptr
	origin        func(uintptr, *int32, *int32) int32
	xid           func(uintptr) uintptr
	strlen        func(*byte) uintptr
	free          func(*byte)
}

var pickerNative struct {
	sync.Once
	gtk pickerGTK
	err error
}

func loadPickerGTK() (*pickerGTK, error) {
	pickerNative.Do(func() {
		// Keep the handle for the process lifetime, like MyGo's GTK backend.
		lib, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			pickerNative.err = err
			return
		}
		g := &pickerNative.gtk
		for symbol, target := range map[string]any{
			"gtk_window_list_toplevels": &g.toplevels, "g_list_length": &g.listLength,
			"g_list_nth_data": &g.listData, "g_list_free": &g.listFree,
			"gtk_file_chooser_get_type": &g.chooserType, "gtk_file_chooser_dialog_get_type": &g.dialogType,
			"gtk_button_get_type": &g.buttonType, "g_type_check_instance_is_a": &g.isType,
			"gtk_widget_get_mapped": &g.mapped,
			"gtk_window_get_title":  &g.title, "gtk_file_chooser_get_current_folder": &g.directory,
			"gtk_file_chooser_get_filename": &g.filename, "gtk_file_chooser_set_current_folder": &g.setDirectory,
			"gtk_file_chooser_unselect_all": &g.unselectAll, "gtk_file_chooser_get_action": &g.action,
			"gtk_window_get_default_widget": &g.defaultWidget, "gtk_dialog_get_response_for_widget": &g.response,
			"gtk_button_get_label": &g.buttonLabel, "gtk_widget_is_sensitive": &g.sensitive,
			"gtk_widget_get_allocated_width": &g.width, "gtk_widget_get_allocated_height": &g.height,
			"gtk_widget_translate_coordinates": &g.translate, "gtk_widget_get_window": &g.window,
			"gdk_window_get_origin": &g.origin, "gdk_x11_window_get_xid": &g.xid,
			"strlen": &g.strlen, "g_free": &g.free,
		} {
			address, err := purego.Dlsym(lib, symbol)
			if err != nil {
				pickerNative.err = fmt.Errorf("picker symbol %s: %w", symbol, err)
				return
			}
			purego.RegisterFunc(target, address)
		}
	})
	return &pickerNative.gtk, pickerNative.err
}

func (g *pickerGTK) takeString(p *byte) string {
	if p == nil {
		return ""
	}
	defer g.free(p)
	return strings.Clone(unsafe.String(p, int(g.strlen(p))))
}

// Main-thread only. Reject ambiguous dialogs instead of selecting a toplevel
// by position or depending on GTK's private widget names/layout.
func (g *pickerGTK) chooser() (uintptr, error) {
	list := g.toplevels()
	defer g.listFree(list)
	var found uintptr
	for i, count := uint32(0), g.listLength(list); i < count; i++ {
		widget := g.listData(list, i)
		if g.mapped(widget) == 0 || g.isType(widget, g.dialogType()) == 0 || g.isType(widget, g.chooserType()) == 0 || g.title(widget) != "Choose checkout or workspace" {
			continue
		}
		if found != 0 {
			return 0, errors.New("multiple native folder choosers")
		}
		found = widget
	}
	return found, nil
}

func (g *pickerGTK) observe(dialog uintptr) pickerState {
	s := pickerState{}
	if dialog == 0 {
		return s
	}
	s.Visible, s.Title, s.Action = true, g.title(dialog), g.action(dialog)
	s.Directory, s.Filename = g.takeString(g.directory(dialog)), g.takeString(g.filename(dialog))
	button, window := g.defaultWidget(dialog), g.window(dialog)
	if window == 0 {
		return s
	}
	s.WindowID = uint64(g.xid(window))
	if button == 0 || g.isType(button, g.buttonType()) == 0 {
		return s
	}
	s.OpenResponse, s.OpenLabel = g.response(dialog, button), g.buttonLabel(button)
	s.OpenSensitive, s.OpenMapped = g.sensitive(button) != 0, g.mapped(button) != 0
	var originX, originY, x, y int32
	if g.origin(window, &originX, &originY) != 0 && g.translate(button, dialog, 0, 0, &x, &y) != 0 {
		s.OpenBounds = pickerBounds{X: originX + x, Y: originY + y, Width: g.width(button), Height: g.height(button)}
	}
	return s
}

func (d *desktopTest) Picker(ctx context.Context) (pickerState, error) {
	return d.picker(ctx, "")
}

func (d *desktopTest) NavigateFolder(ctx context.Context, path string) (pickerState, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return pickerState{}, errors.New("picker requires absolute directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return pickerState{}, err
	}
	if !info.IsDir() {
		return pickerState{}, errors.New("picker requires existing directory")
	}
	return d.picker(ctx, path)
}

func (d *desktopTest) picker(ctx context.Context, path string) (state pickerState, err error) {
	if err = d.desktop.authorize(ctx); err != nil {
		return state, err
	}
	mygo.RunOnMain(func() {
		if err = ctx.Err(); err != nil {
			return
		}
		var g *pickerGTK
		g, err = loadPickerGTK()
		if err != nil {
			return
		}
		var dialog uintptr
		dialog, err = g.chooser()
		if err != nil {
			return
		}
		if path != "" {
			if dialog == 0 || g.action(dialog) != 2 { // GTK_FILE_CHOOSER_ACTION_SELECT_FOLDER
				err = errors.New("no native select-folder dialog")
				return
			}
			// Public navigation clears unfinished location completion. Directory
			// loading is asynchronous; the caller must observe actual readiness.
			g.unselectAll(dialog)
			g.setDirectory(dialog, path)
		}
		state = g.observe(dialog)
	})
	return state, err
}
