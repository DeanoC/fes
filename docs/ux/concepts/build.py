#!/usr/bin/env python3
"""Build the offline FES concept page using only the Python standard library."""

from pathlib import Path
import re


DIRECTORY = Path(__file__).resolve().parent
MARKER = "<!-- FES_CONCEPT_FRAGMENT -->"
SYMBOLS = {
    "monitor": "▣",
    "house": "⌂",
    "arrow-left": "←",
    "info": "i",
    "library": "▤",
    "pencil-ruler": "✎",
    "signpost": "↗",
    "tv": "▣",
    "laptop": "▱",
}


def replace_icon(match: re.Match[str]) -> str:
    name = match.group(1)
    if name not in SYMBOLS:
        raise ValueError(f"Add a local text symbol for icon: {name}")
    return f'<span class="fes-symbol" aria-hidden="true">{SYMBOLS[name]}</span>'


def main() -> None:
    fragment = (DIRECTORY / "fes-rooms.fragment.html").read_text(encoding="utf-8")
    shell = (DIRECTORY / "standalone-shell.html").read_text(encoding="utf-8")
    if shell.count(MARKER) != 1:
        raise ValueError("The standalone shell must contain one fragment marker")
    fragment = re.sub(
        r'<i data-lucide="([a-z-]+)" aria-hidden="true"></i>',
        replace_icon,
        fragment,
    )
    fragment = fragment.replace('class="viz-carousel"', 'class="fes-rooms"')
    fragment = fragment.replace("cursor-interaction", "fes-interactive")
    fragment = fragment.replace(
        "const icons = () => { if (globalThis.lucide) globalThis.lucide.createIcons({attrs:{width:16,height:16}}); };",
        "const icons = () => {}; // Icons are local text symbols in this export.",
    )
    if re.search(r"data-lucide|globalThis\.lucide|window\.openai|globalThis\.openai", fragment):
        raise ValueError("A host-dependent placeholder remains in the export")
    output = DIRECTORY / "fes-rooms.html"
    output.write_text(shell.replace(MARKER, fragment), encoding="utf-8")
    print(f"Built {output.name} ({output.stat().st_size:,} bytes)")


if __name__ == "__main__":
    main()
