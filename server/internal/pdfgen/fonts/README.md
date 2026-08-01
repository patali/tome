Copy of `extension/fonts/` — Go cannot embed across module boundaries, and the
PDF renderer must not fetch fonts over the network. Every family is SIL Open
Font License 1.1; the notices ship alongside. Update both copies together.

Only the family the request actually asks for is inlined into the generated
HTML, so adding families here costs binary size but not per-render size.
