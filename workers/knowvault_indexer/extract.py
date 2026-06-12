"""Text extraction for the KnowVault indexer — turn the document zoo into plain
text for chunking and embedding.

Native (stdlib only): plain text and source code; docx / xlsx / pptx (these are
Office Open XML — a zip of XML, so we unzip and pull the text runs); rtf (strip
the control words). PDF uses pypdf if it is installed (a pure-Python optional
dependency); without it, PDFs are skipped. Legacy binary doc / xls / ppt (OLE)
are not handled here — convert them to docx/pdf, or wire a LibreOffice fallback.

extract_text(path) -> str | None   (None = unsupported or failed; caller skips)
"""
import html
import os
import re
import shutil
import subprocess
import zipfile

_PANDOC = shutil.which("pandoc")
# document formats pandoc reads well to plain text (used as a fallback / for
# formats we don't parse natively). pandoc CANNOT read pdf or legacy binary
# doc/xls/ppt, and does not do spreadsheets/presentations.
PANDOC_EXT = {".docx", ".docm", ".rtf", ".odt", ".epub", ".fb2", ".html", ".htm", ".tex"}

# read these as-is — prose, source code, config, markup
PLAIN_EXT = {
    ".txt", ".md", ".rst", ".csv", ".log", ".adoc", ".org", ".tex",
    ".py", ".go", ".js", ".ts", ".jsx", ".tsx", ".java", ".kt", ".scala",
    ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".rb", ".php", ".rs", ".swift",
    ".sh", ".bash", ".ps1", ".sql", ".r", ".lua", ".dart", ".ex", ".exs",
    ".json", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".env",
    ".xml", ".html", ".htm", ".css", ".scss", ".proto", ".gradle", ".dsl",
}
RICH_EXT = {".docx", ".xlsx", ".pptx", ".rtf", ".pdf"}
# what this machine can actually turn into text (pandoc formats only if installed)
SUPPORTED_EXT = PLAIN_EXT | RICH_EXT | (PANDOC_EXT if _PANDOC else set())
# listed by users but not extractable without LibreOffice (legacy binary OLE)
LEGACY_EXT = {".doc", ".xls", ".ppt"}


def _clean(s):
    return re.sub(r"[ \t]*\n[ \t]*", "\n", html.unescape(s)).strip()


def _read_text(path):
    with open(path, encoding="utf-8", errors="ignore") as f:
        return f.read()


def _docx(path):
    with zipfile.ZipFile(path) as z:
        xml = z.read("word/document.xml").decode("utf-8", "ignore")
    xml = xml.replace("</w:p>", "\n")           # paragraph breaks
    return _clean(re.sub(r"<[^>]+>", "", xml))   # drop every tag, keep text


def _xlsx(path):
    out = []
    with zipfile.ZipFile(path) as z:
        names = z.namelist()
        for part in ["xl/sharedStrings.xml"] + [n for n in names if n.startswith("xl/worksheets/") and n.endswith(".xml")]:
            if part not in names:
                continue
            s = z.read(part).decode("utf-8", "ignore")
            out += re.findall(r"<t[^>]*>(.*?)</t>", s, re.S)
    return _clean("\n".join(out))


def _pptx(path):
    out = []
    with zipfile.ZipFile(path) as z:
        for n in sorted(z.namelist()):
            if n.startswith("ppt/slides/slide") and n.endswith(".xml"):
                s = z.read(n).decode("utf-8", "ignore")
                out += re.findall(r"<a:t>(.*?)</a:t>", s, re.S)
    return _clean("\n".join(out))


def _rtf(path):
    with open(path, encoding="utf-8", errors="ignore") as f:
        data = f.read()
    data = re.sub(r"\\'[0-9a-fA-F]{2}", " ", data)   # hex-escaped chars
    data = re.sub(r"\\[a-zA-Z]+-?\d* ?", " ", data)  # control words
    data = re.sub(r"[{}]|\\[^a-zA-Z]", "", data)      # groups + symbol escapes
    return re.sub(r"[ \t]+", " ", data).strip()


def _pdf(path):
    try:
        from pypdf import PdfReader
    except ImportError:
        return None
    reader = PdfReader(path)
    return _clean("\n".join((p.extract_text() or "") for p in reader.pages))


def _pandoc(path):
    """Convert a document to plain text with pandoc (if installed). Handles docx,
    rtf, odt, epub, html, latex — pandoc auto-detects the input by extension."""
    if not _PANDOC:
        return None
    try:
        r = subprocess.run([_PANDOC, "-t", "plain", "--wrap=none", path],
                           capture_output=True, timeout=120)
        if r.returncode == 0:
            return _clean(r.stdout.decode("utf-8", "ignore"))
    except (OSError, subprocess.SubprocessError):
        pass
    return None


_DISPATCH = {".docx": _docx, ".xlsx": _xlsx, ".pptx": _pptx, ".rtf": _rtf, ".pdf": _pdf}


def extract_text(path):
    ext = os.path.splitext(path)[1].lower()
    try:
        if ext in PLAIN_EXT:
            return _read_text(path)
        fn = _DISPATCH.get(ext)
        if fn:
            txt = fn(path)
            if txt and txt.strip():
                return txt
        # fallback / odt,epub,html,…: let pandoc try if it knows the format
        if _PANDOC and ext in PANDOC_EXT:
            return _pandoc(path)
    except (OSError, zipfile.BadZipFile, KeyError, ValueError):
        return None
    return None
