"""Build a standalone HTTP-only frontend; never publish the working tree."""
from pathlib import Path
from html.parser import HTMLParser
from html import escape
from urllib.parse import urlsplit, unquote
import hashlib
import json
import re
import shutil

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'dist'
OMIT = {'js/config.js', 'js/demo-credentials.js', 'js/admin-credentials.js', 'data/residents.js', 'js/seed-data.js'}
SOURCE_URLS = (('静态资源/', 'assets/'), ('样式/', 'css/'), ('数据/', 'data/'), ('脚本/', 'js/'))
VOID = {'area','base','br','col','embed','hr','img','input','link','meta','param','source','track','wbr'}


def release_path(path):
    for source, target in SOURCE_URLS:
        path = path.replace(source, target)
    return path


class ReleasePage(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=False)
        self.output, self.refs = [], []
        self.depth = 0

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if self.depth:
            if tag not in VOID: self.depth += 1
            return
        if 'data-demo-only' in attrs or (tag == 'script' and release_path(attrs.get('src', '')) in OMIT):
            if tag not in VOID: self.depth = 1
            return
        raw = self.get_starttag_text()
        raw = release_path(raw)
        if 'data-http-only' in attrs: raw = re.sub(r'\s+hidden(?:="[^"]*")?', '', raw)
        self.output.append(raw)
        for key in ('src','href'):
            if attrs.get(key): self.refs.append(release_path(attrs[key]))
        if 'data-http-text' in attrs and tag not in VOID:
            self.output.append(escape(attrs['data-http-text']) + '</' + tag + '>')
            self.depth = 1

    def handle_startendtag(self, tag, attrs):
        self.handle_starttag(tag, attrs)

    def handle_endtag(self, tag):
        if self.depth:
            self.depth -= 1
        else:
            self.output.append('</' + tag + '>')

    def handle_data(self, data):
        if not self.depth: self.output.append(data)

    def handle_decl(self, decl):
        if not self.depth: self.output.append('<!' + decl + '>')

    def handle_entityref(self, name):
        self.handle_data('&' + name + ';')

    def handle_charref(self, name):
        self.handle_data('&#' + name + ';')


def build():
    # Only the known build directory can be replaced. Refuse symlinks/junctions.
    if OUT.is_symlink() or OUT.resolve() != ROOT.resolve() / 'dist':
        raise SystemExit('Unsafe output directory')
    if OUT.exists(): shutil.rmtree(OUT)
    OUT.mkdir()
    for source, target in (('静态资源','assets'),('样式','css'),('脚本','js')):
        shutil.copytree(ROOT / source, OUT / target)
    (OUT / 'data').mkdir()
    image_map = (ROOT / '数据/image-map.js').read_text(encoding='utf-8-sig')
    (OUT / 'data/image-map.js').write_text(image_map.replace('静态资源/img/', 'assets/img/'), encoding='utf-8')
    for path in (OUT / 'css').rglob('*.css'):
        text = path.read_text(encoding='utf-8-sig')
        path.write_text(text.replace('静态资源/', 'assets/'), encoding='utf-8')
    for name in ('site.js','theme.js'):
        shutil.copyfile(ROOT / '脚本' / name, OUT / name)
    for name in OMIT:
        (OUT / name).unlink(missing_ok=True)
    (OUT / 'js/design-preview.js').unlink(missing_ok=True)
    for name in ('brand-tokens.css','common.css','design-preview.css','tokens.css'):
        (OUT / 'css' / name).unlink(missing_ok=True)
    config = (OUT / 'js/care-runtime-config.js').read_text(encoding='utf-8-sig')
    default_mode = "mode:'demo'"
    allowed_modes = "['demo','http']"
    if config.count(default_mode) != 1 or config.count(allowed_modes) != 1:
        raise SystemExit('Expected exactly one demo default and one demo/http mode allow-list')
    config = config.replace(default_mode, "mode:'http'", 1).replace(allowed_modes, "['http']", 1)
    if config.count("mode:'http'") != 1 or config.count("['http']") != 1:
        raise SystemExit('HTTP runtime mode replacement did not produce the expected config')
    (OUT / 'js/care-runtime-config.js').write_text(config, encoding='utf-8')
    artifact_config = (OUT / 'js/care-runtime-config.js').read_text(encoding='utf-8')
    if artifact_config.count("mode:'http'") != 1 or artifact_config.count("['http']") != 1:
        raise SystemExit('Generated frontend runtime mode is not HTTP-only')
    (OUT / 'js/login-page.js').write_text('CareLoginHttp.attach();' + chr(10), encoding='utf-8')
    # Export only public labels and the HTTP adapter, without local seed/mutation code.
    source = (ROOT / '脚本/care-api.js').read_text(encoding='utf-8-sig')
    metadata = []
    for key in ('TYPES','STATES','METRICS'):
        match = re.search(r'  const '+key+r' = (.+);', source)
        if not match: raise SystemExit('Missing API metadata: ' + key)
        metadata.append('  const ' + key + ' = ' + match.group(1) + ';')
    api = "(() => {\n" + '\n'.join(metadata) + "\n  const isOpen=e=>!['resolved','false_alarm'].includes(e.state);\n  window.CareAPI=CareHttpAdapter.create({TYPES,STATES,METRICS,isOpen});\n})();\n"
    (OUT / 'js/care-api.js').write_text(api, encoding='utf-8')
    (OUT / 'js/admin-login.js').write_text('var AdminLogin = CareSession.staffGate();\n', encoding='utf-8')
    pages = []
    for source in ROOT.glob('*.html'):
        if source.name == 'design-preview.html': continue
        parser = ReleasePage()
        parser.feed(source.read_text(encoding='utf-8-sig'))
        if parser.depth: raise SystemExit('Unclosed release markup: ' + source.name)
        (OUT / source.name).write_text(''.join(parser.output), encoding='utf-8')
        pages.append((source.name, parser.refs))
    for name, refs in pages:
        for ref in refs:
            target = urlsplit(ref)
            if not target.scheme and not target.netloc and target.path and not (OUT / unquote(target.path)).is_file():
                raise SystemExit('Missing release reference: '+name+' -> '+ref)
    shutil.copyfile(ROOT / '部署/headers', OUT / '_headers')
    manifest = {str(p.relative_to(OUT)).replace('\\','/'):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(OUT.rglob('*')) if p.is_file()}
    (OUT / 'release-manifest.json').write_text(json.dumps({'mode':'http','files':manifest},ensure_ascii=False,indent=2),encoding='utf-8')
    print('PASS: HTTP-only release in dist; mode=http; demo substitutions=1+1; all page references resolved')


if __name__ == '__main__':
    build()
