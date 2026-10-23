import { createCn } from "cn/config";

// Teach class merging the token names registered in style.css's theme bridge,
// so `text-code` is a size (not a color) and `h-control` replaces `h-control-small`.
export const cn = createCn({
  extend: {
    theme: {
      text: ["base", "code", "input-touch"],
      spacing: [
        "control",
        "control-small",
        "control-compact",
        "control-large",
        "touch",
        "panel-header",
        "topbar",
        "icon",
        "icon-sm",
        "icon-lg",
      ],
      radius: ["control"],
      leading: ["ui", "copy", "tree"],
      tracking: ["heading", "label"],
      shadow: ["overlay"],
    },
  },
});
