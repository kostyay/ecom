# Bike-Discount Offline Catalog Fixtures

These fixtures support deterministic tests with no network access.

The evidence date and market are in `manifest.json`. The direct `llms.txt` file preserves verified official root links. The other files are small, sanitized forms of facts and structures in `docs/providers/bike-discount-research.md`. They are not raw page archives.

A `data-fixture-placeholder` attribute identifies test-only content. A parser can read this content in a boundary test. Provider code must not treat the placeholder value as a current catalog fact or as a stable website selector.

The legacy search file proves only the old indexed request form and heading. The current search file records the verified `search` query parameter and current product-card structure. The controls file uses marked zero IDs because current category-specific IDs were not captured. Provider code must get real filter values from a current listing.

The files do not contain browser state, access-control identifiers, analytics identifiers, personal data, or delivery charges. Displayed item prices stay unchanged. The security test checks these limits and checks every file hash.
