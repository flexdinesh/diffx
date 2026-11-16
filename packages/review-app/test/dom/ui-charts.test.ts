import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { createElement } from "react";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import {
  BarChart,
  type BarChartBin,
  type BarChartSeries,
} from "@diffx/ui/components/bar-chart";
import { ReadoutStrip } from "@diffx/ui/components/readout-strip";
import { TooltipProvider } from "@diffx/ui/components/ui/tooltip";

afterEach(cleanup);

const series: BarChartSeries[] = [
  { name: "merged", mark: "solid" },
  { name: "opened", mark: "hollow" },
];

const bins: BarChartBin[] = [
  { id: "a", label: "Fri 7 Nov", values: [0, 0], unavailable: true },
  { id: "b", label: "Sat 8 Nov", tick: "Sat 8", values: [3, 5] },
  { id: "c", label: "Sun 9 Nov", tick: "Sun 9", values: [0, 1] },
  { id: "d", label: "Mon 10 Nov", values: [7, 2], unavailable: true },
  { id: "e", label: "Tue 11 Nov", values: [4, 4] },
  { id: "f", label: "Wed 12 Nov", values: [0, 0], disabled: true },
];

function chart(props: {
  selected?: string;
  onActivate?: (id: string) => void;
  marker?: { index: number; fraction: number; label: string };
}) {
  render(
    createElement(
      TooltipProvider,
      null,
      createElement(BarChart, {
        label: "Activity, 7 – 12 Nov",
        series,
        bins,
        ...props,
      }),
    ),
  );
  return screen.getByRole("group", { name: "Activity, 7 – 12 Nov" });
}

function bin(group: HTMLElement, name: string): HTMLElement {
  return within(group).getByRole("button", { name });
}

test("bar chart names each bin with its label and series counts", () => {
  const group = chart({});
  assert.ok(bin(group, "Sat 8 Nov: 3 merged, 5 opened"));
  assert.ok(bin(group, "Sun 9 Nov: 0 merged, 1 opened"));
  assert.ok(bin(group, "Tue 11 Nov: 4 merged, 4 opened"));
  const future = bin(group, "Wed 12 Nov: 0 merged, 0 opened");
  assert.equal(future.hasAttribute("disabled"), true);
  const missing = bin(group, "Fri 7 Nov: not available");
  assert.equal(missing.getAttribute("aria-disabled"), "true");
  // Without an activation handler bins are not toggle buttons.
  assert.equal(
    bin(group, "Sat 8 Nov: 3 merged, 5 opened").hasAttribute("aria-pressed"),
    false,
  );
});

test("bar chart keeps one tab stop and arrows skip unavailable and disabled bins", () => {
  const group = chart({ selected: "c", onActivate: () => undefined });
  const stops = within(group)
    .getAllByRole("button")
    .filter((button) => button.tabIndex === 0);
  assert.deepEqual(
    stops.map((button) => button.getAttribute("aria-label")),
    ["Sun 9 Nov: 0 merged, 1 opened"],
  );

  const sun = bin(group, "Sun 9 Nov: 0 merged, 1 opened");
  const sat = bin(group, "Sat 8 Nov: 3 merged, 5 opened");
  const tue = bin(group, "Tue 11 Nov: 4 merged, 4 opened");
  act(() => sun.focus());
  fireEvent.keyDown(sun, { key: "ArrowRight" });
  assert.equal(document.activeElement, tue);
  assert.equal(tue.tabIndex, 0);
  assert.equal(sun.tabIndex, -1);

  // The last bin is disabled and the first unavailable: the ends stay put.
  fireEvent.keyDown(tue, { key: "ArrowRight" });
  assert.equal(document.activeElement, tue);
  fireEvent.keyDown(tue, { key: "Home" });
  assert.equal(document.activeElement, sat);
  fireEvent.keyDown(sat, { key: "ArrowLeft" });
  assert.equal(document.activeElement, sat);
  fireEvent.keyDown(sat, { key: "End" });
  assert.equal(document.activeElement, tue);
});

test("bar chart activates available bins and marks the selected one pressed", () => {
  const activated: string[] = [];
  const group = chart({
    selected: "b",
    onActivate: (id) => activated.push(id),
  });
  const sat = bin(group, "Sat 8 Nov: 3 merged, 5 opened");
  const tue = bin(group, "Tue 11 Nov: 4 merged, 4 opened");
  assert.equal(sat.getAttribute("aria-pressed"), "true");
  assert.equal(tue.getAttribute("aria-pressed"), "false");

  fireEvent.click(tue);
  fireEvent.click(bin(group, "Mon 10 Nov: not available"));
  fireEvent.click(bin(group, "Wed 12 Nov: 0 merged, 0 opened"));
  assert.deepEqual(activated, ["e"]);
});

test("bar chart announces the now marker", () => {
  const group = chart({
    marker: { index: 4, fraction: 0.5, label: "Now, Tue 11 Nov 12:00" },
  });
  assert.ok(within(group).getByText("Now, Tue 11 Nov 12:00"));
});

test("readout strip lists each value with its label", () => {
  render(
    createElement(ReadoutStrip, {
      label: "Activity summary",
      cells: [
        { value: "47", label: "merged" },
        { value: "12", label: "open now" },
      ],
    }),
  );
  const list = screen.getByRole("list", { name: "Activity summary" });
  assert.deepEqual(
    within(list)
      .getAllByRole("listitem")
      .map((item) => item.textContent),
    ["47 merged", "12 open now"],
  );
});
