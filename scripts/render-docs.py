#!/usr/bin/env python3
"""Refresh committed static documentation: python -m pip install Markdown==3.11.3."""
import html
from pathlib import Path
import re
from urllib.parse import urlsplit, urlunsplit

import markdown
from markdown.extensions.toc import slugify_unicode

ROOT = Path(__file__).resolve().parents[1]
DOCS = ROOT / "docs"
OUT = ROOT / "website/static/docs"
LOCALES = {
    "en": ("English", "Documentation", "Setup", "Resources", "Examples", "On this page"),
    "ja": ("日本語", "文書", "設定手順", "リソース一覧", "設定例", "このページの内容"),
    "zh-Hant": ("繁體中文", "文件", "設定步驟", "資源一覽", "設定範例", "本頁內容"),
    "zh-Hans": ("简体中文", "文档", "配置步骤", "资源列表", "配置示例", "本页内容"),
}


def doc_url(path):
    return "/docs/" + path.with_suffix(".html").as_posix()


def rewrite(url, source):
    prefix = "https://github.com/imksoo/routerd/blob/main/"
    if url.startswith(prefix + "docs/"):
        url = "/" + url[len(prefix):]
    elif url.startswith(prefix + "examples/"):
        return "/" + url[len(prefix):]
    parsed = urlsplit(url)
    if parsed.scheme or parsed.netloc or not parsed.path:
        return url
    if parsed.path.endswith(".md"):
        return urlunsplit(parsed._replace(path=parsed.path[:-3] + ".html"))
    if parsed.path.startswith("/docs/") and (ROOT / (parsed.path.lstrip("/") + ".md")).exists():
        return urlunsplit(parsed._replace(path=parsed.path + ".html"))
    return url


expected = {OUT / source.relative_to(DOCS).with_suffix(".html") for source in DOCS.rglob("*.md")}
for old in OUT.rglob("*.html"):
    if old not in expected:
        old.unlink()

for source in sorted(DOCS.rglob("*.md")):
    rel = source.relative_to(DOCS)
    lang = rel.parts[0] if rel.parts[0] in LOCALES else "en"
    translated = Path(*rel.parts[1:]) if lang != "en" else rel
    labels = LOCALES[lang]
    base = Path(lang) if lang != "en" else Path()
    home = "/" if lang == "en" else f"/{lang}/"
    text = source.read_text()
    text = re.sub(r"\A---\n.*?\n---\n", "", text, flags=re.S)
    md = markdown.Markdown(extensions=["fenced_code", "tables", "toc"], extension_configs={"toc": {"slugify": slugify_unicode, "toc_depth": "2-3"}})
    body = md.convert(text)
    body = re.sub(r'href="([^"<>]+)"', lambda m: 'href="' + html.escape(rewrite(html.unescape(m[1]), source), quote=True) + '"', body)
    heading = re.search(r"<h1[^>]*>(.*?)</h1>", body, re.S)
    title = re.sub("<[^>]+>", "", heading[1]) if heading else source.stem
    languages = []
    for locale, words in LOCALES.items():
        target = (Path(locale) / translated) if locale != "en" else translated
        if not (DOCS / target).exists():
            target = (Path(locale) / "intro.md") if locale != "en" else Path("intro.md")
        current = ' aria-current="page"' if locale == lang else ""
        languages.append(f'<a href="{doc_url(target)}" lang="{locale}" hreflang="{locale}"{current}>{words[0]}</a>')
    links = [("intro.md", labels[1]), ("install-and-upgrade.md", labels[2]), ("reference/resources.md", labels[3]), ("config-examples/index.md", labels[4])]
    nav = "".join(f'<a href="{doc_url(base / path)}">{label}</a>' for path, label in links)
    page = f'''<!doctype html>
<html lang="{lang}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{title} · routerd</title><link rel="stylesheet" href="/site.css"><link rel="canonical" href="https://routerd.net{doc_url(rel)}"></head>
<body><header><nav class="wrap"><a class="brand" href="{home}">routerd</a><div class="navlinks">{nav}</div><div class="languages">{"".join(languages)}</div></nav></header>
<main class="doclayout wrap"><article class="document">{body}</article><aside class="doccontents"><strong>{labels[5]}</strong>{md.toc}</aside></main>
<footer><div class="wrap footerrow"><span>routerd · BSD 3-Clause</span><a href="https://github.com/imksoo/routerd/blob/main/docs/{rel.as_posix()}">GitHub</a></div></footer></body></html>
'''
    target = OUT / rel.with_suffix(".html")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(page)
print("Static documentation refreshed")
