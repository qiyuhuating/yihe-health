"""Check JS syntax, duplicate HTML IDs and missing local source/link targets."""
import pathlib
import subprocess
from collections import Counter
from html.parser import HTMLParser
from urllib.parse import unquote, urlsplit

ROOT=pathlib.Path(__file__).resolve().parents[1]
errors=[]
class Page(HTMLParser):
    def __init__(self):
        super().__init__();self.ids=[];self.refs=[]
    def handle_starttag(self,tag,attrs):
        attrs=dict(attrs)
        if attrs.get('id'):self.ids.append(attrs['id'])
        for key in ['src','href']:
            if attrs.get(key):self.refs.append(attrs[key])

files=list(ROOT.glob('*.js'))+list((ROOT/'js').glob('*.js'))+list((ROOT/'data').glob('*.js'))
for path in files:
    result=subprocess.run(['node','--check',str(path)],capture_output=True,text=True)
    if result.returncode:errors.append(result.stderr)
pages=list(ROOT.glob('*.html'))
for path in pages:
    page=Page();page.feed(path.read_text(encoding='utf-8-sig'))
    errors.extend(f'{path.name}: duplicate id {key}' for key,n in Counter(page.ids).items() if n>1)
    for ref in page.refs:
        parsed=urlsplit(ref)
        if parsed.scheme or parsed.netloc or not parsed.path:continue
        if not (path.parent/unquote(parsed.path)).exists():errors.append(f'{path.name}: missing {ref}')
if errors:raise SystemExit('\n'.join(errors))
print(f'PASS: {len(files)} JavaScript files, {len(pages)} HTML pages')
