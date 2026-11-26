# Web font notices

Latin letters and numerals use unmodified Adobe Source Serif 4 variable WOFF2 (Roman and Italic):
https://github.com/adobe-fonts/source-serif
License: source-serif-LICENSE.md

Chinese uses web subsets derived from the official Adobe Source Han Serif SC variable font:
https://github.com/adobe-fonts/source-han-serif
License: source-han-serif-LICENSE.txt

The Chinese web subsets are identified internally as Care Han Serif SC to distinguish these modified web files from the original font, whose reserved font name is Source. This is a subset-distribution alias, not a replacement typeface design. Glyph outlines and weight variations are retained, with coverage of the upstream CJK repertoire (code points >= U+2E80). Latin characters are supplied by Source Serif 4. Adobe authorship, copyright and OFL metadata are retained.

Subset distribution: 85 WOFF2 pieces with non-overlapping CSS unicode ranges; the common interface subset loads first. Other available CJK characters load their corresponding piece on demand. Generated manifest: source-han-serif-sc/manifest.json. Upstream source identity and per-part coverage are recorded in the manifest.

OFL subsetting and reserved names guidance: https://openfontlicense.org/ofl-faq/#2-6-is-subsetting-a-webfont-considered-modification
