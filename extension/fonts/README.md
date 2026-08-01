# Bundled fonts

Both families are licensed under the **SIL Open Font License 1.1**, which
permits bundling and redistribution — including commercially — provided the
copyright and license notice travel with the files. They are kept here in
full: `OFL-Literata.txt` and `OFL-JetBrainsMono.txt`.

| Family | Copyright | Upstream |
|---|---|---|
| Literata | 2017 The Literata Project Authors | https://github.com/googlefonts/literata |
| JetBrains Mono | 2020 The JetBrains Mono Project Authors | https://github.com/JetBrains/JetBrainsMono |

Only the **latin** and **latin-ext** subsets are bundled (6 `.woff2` files,
~285 KB). Other scripts fall back to the system serif/mono — the tradeoff for
not shipping every subset Google serves. Both are variable fonts, so one file
per style covers every weight the stylesheet asks for.

The `@font-face` rules live in `../fonts.css`, generated from the Google Fonts
CSS2 API with the remote URLs rewritten to these local files; the
`unicode-range` and weight declarations are Google's, unmodified.

The server keeps its own copy under `server/internal/pdfgen/fonts/` because Go
cannot embed across module boundaries. Update both together.
