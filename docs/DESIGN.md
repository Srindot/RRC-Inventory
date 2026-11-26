# Design guide

The site should look like a calm, professional tool, not a demo. The frame is
the lab's original design: the RRC logo and name across the top, a home page of
large tiles ("What would you like to do?"), and a "Back to Home" link on every
other page. Inside a page: a grid of quiet cards with small labels and big
numbers. Colour is Rosé Pine (dark by default) and Rosé Pine Dawn (light),
switched with the Dark/Light button in the header and remembered per browser.

Everything below already exists in `frontend/src/lib/theme.css`,
`Shell.svelte` and `Icon.svelte`. Build pages from these pieces instead of
adding new styles.

## Layout

- The shell (`lib/Shell.svelte`, mounted in `+layout.svelte`) draws the header
  (logo, title, theme button, notifications, account), the "Back to Home" link
  on every page but home, and the footer. It is the same on phones; the header
  wraps and the home tiles stack into one column. Pages never add their own
  navigation, logo, back links or account links.
- New areas of the site get a tile on the home page (`routes/+page.svelte`,
  `.option-btn` with an emoji and a `--tile-accent` Rosé Pine colour).
- A page is `<div class="page">` with a `<header class="page-head">` holding an
  `<h1>` and at most one muted line under it, then content.
- Content is a `.grid` of `.card`s. A card starts with a small `.label`, then a
  big `.stat`, a short sentence, or a list of `.row`s.
- At most one filled card (`.card.accent` or `.card.filled`) per page, for the
  single most important thing. `.card.hued` / `.card.tint` for highlights.

## Colour

Use only these tokens; never hex or rgba literals (shadows excepted):

| Token | Use |
|---|---|
| `--bg` | page canvas |
| `--card`, `--card-raised` | cards; inputs, menus, toasts |
| `--line`, `--line-strong` | hairline borders, dividers, hover fills |
| `--ctp-text`, `--muted`, `--faint` | text, secondary text, tertiary text |
| `--accent`, `--accent-soft` | the one accent (Rosé Pine iris) and its tint |
| `--accent-ink` | text on a filled accent |
| `--danger`, `--warn` | errors, overdue; warnings |

### Colour with meaning

Colour is used to say *what something is*, not to decorate. Each area of the
site owns a hue (`lib/palette.js` → `AREA`; the `--ctp-*` names there are
aliases onto Rosé Pine, see `theme.css`), and each research
thread or robot gets a stable hue from `hueFor(id)`:

| Area | Hue |
|---|---|
| Home | mauve |
| Dashboard | blue |
| Mech Lab | peach |
| MoCap Lab | pink |
| Equipment | teal |
| Research threads | green |
| 3D printers | sapphire |
| Admin | red |

Set the hue on an element with `style="--hue: …"`; these pieces use it:
`.icon-badge` (tinted square behind an icon), `.dot`, `.card.hued` (tinted
card, coloured label), `.card.filled` (solid, for the one key figure),
`.chip.hue`, `.bar > span`, `.hue-text`. A thread's bookings, card and hours
bar share its hue everywhere. Status colours stay semantic: `--danger` for
overdue/errors, `--warn` (yellow) for waiting/attention. Labs: Main Lab blue,
Mech Lab peach, Control Lab flamingo (`LAB` in `lib/palette.js`).

Light and dark mode both have to work, so nothing may assume a dark background.

## Type and numbers

- Sentence case everywhere. No emoji in headings, labels, buttons or nav. No
  exclamation marks, no ALL CAPS.
- Times, dates, counts and durations use `.mono` (tabular figures).
- Copy is short and plain: "3 loans overdue", not "Uh-oh! You have 3 overdue
  loans 😬".

## Controls

- `.btn` for actions, `.btn.primary` for the one main action in a view,
  `.btn.danger` for destructive ones. Minimum height 40px.
- `.chip`, `.chip.ok`, `.chip.warn` for status.
- `.bar > span` for progress (width in %).
- Feedback goes in the global toast: `<div class="message success|error">`.
- Icons come from `lib/Icon.svelte` (line icons). Add a path there if one is
  missing; don't add an icon library.

## Don't

Gradients, glows, coloured shadows, glassmorphism on content, hover lifts,
bouncing animations, decorative emoji, colour that doesn't stand for anything.
