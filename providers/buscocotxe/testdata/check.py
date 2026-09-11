"""Check saved fixture contents and provenance without network access."""

import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path
import re
from urllib.parse import urlsplit


class Document(HTMLParser):
    def __init__(self, source):
        super().__init__()
        self.ids = []
        self.text = []
        self.feed(source)

    def handle_starttag(self, tag, attrs):
        assert tag not in {"script", "form", "input", "iframe", "img"}, tag
        attrs = dict(attrs)
        assert not any(key.startswith("on") for key in attrs), attrs
        assert "kmk-seguiment-idprofessional" not in attrs
        if attrs.get("kmk-seguiment") == "llistat":
            self.ids.append(attrs.get("kmk-seguiment-iditem"))
        if "href" in attrs:
            url = urlsplit(attrs["href"])
            assert url.scheme in {"", "https"}, url
            assert url.netloc in {"", "www.buscocotxe.ad"}, url
            assert not url.username and not url.password, url

    def handle_data(self, data):
        self.text.append(data)


def check():
    root = Path(__file__).parent
    manifest = json.loads((root / "manifest.json").read_text())
    fixtures = manifest["fixtures"]
    assert len({f["id"] for f in fixtures}) == len(fixtures)
    assert {f["path"] for f in fixtures} == {p.name for p in root.glob("*.html")}
    pages = {}
    for fixture in fixtures:
        path = root / fixture["path"]
        data = path.read_bytes()
        assert hashlib.sha256(data).hexdigest() == fixture["sha256"], path
        source = data.decode("utf-8")
        document = Document(source)
        text = " ".join(" ".join(document.text).split())
        assert not re.search(r"[\w.+-]+@[\w.-]+\.[a-z]{2,}", text, re.I), path
        assert not re.search(r"\+376\s*[\d\s]{6,}", text), path
        assert not any(value in source.lower() for value in (
            "mailto:", "tel:", "recaptcha", "googletagmanager", "password",
        )), path
        expected = fixture["expected"]
        assert document.ids == expected["ids"], path
        assert len(document.ids) == expected["card_count"], path
        if fixture["provenance"] == "synthetic_regression":
            assert 'data-fixture-placeholder="true"' in source, path
            assert fixture["source_url"] is None and fixture["captured_at"] is None
        else:
            assert len(document.ids) == len(set(document.ids)), path
            assert all(value.isdigit() for value in document.ids), path
            assert fixture["source_url"].startswith("https://www.buscocotxe.ad/")
            if fixture["provenance"] == "carfinder_saved_fixture":
                assert fixture["captured_at"] is None and fixture["source_commit"]
            else:
                assert fixture["captured_at"].startswith("2026-09-11")
            if expected["number"] is None:
                assert "No s'han trobat resultats." in text, path
                assert not document.ids, path
            else:
                match = re.search(r"([\d.]+) resultats\. Mostrant pàgina (\d+) de (\d+)", text)
                assert match, path
                actual = tuple(int(v.replace(".", "")) for v in match.groups())
                assert actual == (expected["total_items"], expected["number"], expected["total_pages"]), path
                assert expected["has_next"] == (actual[1] < actual[2]), path
            if "first_item" in expected:
                assert expected["first_item"]["id"] == document.ids[0], path
                assert expected["first_item"]["name"] in text, path
        pages[fixture["id"]] = fixture
    assert set(pages["search_page_1"]["expected"]["ids"]).isdisjoint(
        pages["search_page_2"]["expected"]["ids"]
    )
    assert pages["search_out_of_range"]["requested_page"] == 43
    assert pages["search_out_of_range"]["expected"]["number"] == 42
    assert pages["search_last"]["sha256"] == pages["search_out_of_range"]["sha256"]
    assert pages["search_page_1"]["expected"]["price_on_request_ids"]
    assert len(pages["search_last"]["expected"]["sold_ids"]) == 7
    print(f"Checked {len(fixtures)} fixtures: hashes, provenance, card IDs, page metadata, and sanitization.")


if __name__ == "__main__":
    check()
