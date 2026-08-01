# Bundled fonts

Every family here is licensed under the **SIL Open Font License 1.1**, which
permits bundling and redistribution — including commercially — provided the
copyright and licence notice travel with the files. Each notice is kept in
full as `OFL-<Family>.txt`.

| Family | Role | Why it's here |
|---|---|---|
| Literata | serif — **default** | Designed by TypeTogether for Google Play Books; a text face built for e-readers. |
| Source Serif 4 | serif | Adobe's text serif. Slightly narrower than Literata, so more words per page. |
| Merriweather | serif | Large x-height and sturdy strokes — the most forgiving of the set on low-contrast e-ink. |
| Libre Baskerville | serif | Baskerville redrawn for screen body text; wider, more traditional book feel. |
| Inter | sans | Neutral, even colour on the page. |
| Atkinson Hyperlegible | sans | Drawn by the Braille Institute to maximise letter distinction for low vision. |
| JetBrains Mono | mono | Code blocks, regardless of the body face. |

Only the **latin** and **latin-ext** subsets are bundled; other scripts fall
back to the system stack. Literata and JetBrains Mono are requested with the
axes they originally shipped with, and their files are byte-identical to the
pre-bundling versions — the default rendering is unchanged. The other families
are requested without the optical-size axis, which cuts 30–45% of their weight
for no visible difference at body size.

`@font-face` rules live in `../fonts.css`, generated from the Google Fonts CSS2
API with remote URLs rewritten to these files; `unicode-range` and weight
declarations are Google's, unmodified.

The server keeps its own copy under `server/internal/pdfgen/fonts/` because Go
cannot embed across module boundaries. Update both together.
