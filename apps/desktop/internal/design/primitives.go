package design

import "github.com/egoist/mygo/ui"

type TextRole uint8

const (
	Body TextRole = iota
	Secondary
	Muted
	Heading
	Code
)

type ButtonRole uint8

const (
	Default ButtonRole = iota
	Primary
	Quiet
	Destructive
)

type StatusRole uint8

const (
	StatusNeutral StatusRole = iota
	StatusSuccess
	StatusWarning
	StatusError
)

func Text(c *ui.Context, text string, role TextRole) ui.Element {
	t := c.Theme()
	e := ui.Text(c, text)
	switch role {
	case Secondary:
		color := t.TextMuted
		for _, choice := range Themes {
			if choice.ID == "system" {
				continue
			}
			p := PaletteFor(choice.ID, t.Dark)
			if p.Color("bg") == t.Background && p.Color("fg") == t.Text && p.Color("accent") == t.Accent {
				color = p.Color("text-secondary")
				break
			}
		}
		e = e.TextColor(color)
	case Muted:
		e = e.TextColor(t.TextMuted).FontSize(t.FontSize * 12 / 14)
	case Heading:
		e = e.FontWeight(600)
	case Code:
		e = e.Font("monospace").FontSize(t.FontSize * 13 / 14)
	}
	return e
}

// Keep MyGo's native interaction styling: Selected, Disabled, keyboard
// activation and focus remain meaningful when consumers compose these controls.
func Button(c *ui.Context, label string, role ButtonRole) ui.Element {
	t := c.Theme()
	p := PaletteFor("system", t.Dark)
	var e ui.Element
	if role == Primary {
		e = ui.PrimaryButton(c, label)
	} else {
		e = ui.Button(c, label)
	}
	e = e.Label(label).MinHeight(p.Metric("control-compact")).Padding(t.Space(1), t.Space(2)).Radius(t.Radius)
	if role == Quiet {
		e = e.Border(0, ui.Transparent)
	}
	if role == Destructive {
		e = e.TextColor(t.Danger)
	}
	return e
}

func Toolbar(c *ui.Context) ui.Element {
	t := c.Theme()
	p := PaletteFor("system", t.Dark)
	return ui.Row(c).Role(ui.RoleToolbar).FocusGroup(ui.Horizontal).AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(1), t.Space(3)).MinHeight(p.Metric("panel-header-height")).Background(t.Surface)
}
func Status(c *ui.Context, label string, role StatusRole) ui.Element {
	t := c.Theme()
	color := t.TextMuted
	switch role {
	case StatusSuccess:
		color = t.Success
	case StatusWarning:
		color = t.Warning
	case StatusError:
		color = t.Danger
	}
	return ui.Text(c, label).TextColor(color).FontSize(t.FontSize * 12 / 14).Label(label)
}
