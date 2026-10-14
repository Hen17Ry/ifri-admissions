"""Document layout hypotheses, independent of eligibility and numerical values."""

from dataclasses import dataclass, replace

from rapidfuzz.fuzz import partial_ratio, ratio


@dataclass(frozen=True)
class TranscriptLayoutProfile:
    name: str
    section: str | None = None
    bounds: tuple[float, float] | None = None


GENERIC = TranscriptLayoutProfile("UNKNOWN_GENERIC")
BAC_GENERAL = TranscriptLayoutProfile("BENIN_BAC_GENERAL", "FIRST_GROUP_WRITTEN")


def select_section(tokens):
    # Import locally: the token geometry is shared with the extraction module.
    from ifri_ocr.extraction import lines, normalize, text_scale_tokens

    original_words = " ".join(normalize(t.text) for t in tokens)
    tokens = text_scale_tokens(tokens)
    rows = lines(tokens)
    texts = [(row, normalize(" ".join(t.text for t in row))) for row in rows]
    written = [
        row
        for row, text in texts
        if any(ratio(word, "ecrites") >= 75 for word in text.split())
        and "premier" in text
        and partial_ratio("epreuves ecrites du premier groupe", text) >= 80
    ]
    if not written:
        for index in range(len(texts) - 1):
            row, text = texts[index]
            following, next_text = texts[index + 1]
            gap = min(t.box[1] for t in following) - max(t.box[3] for t in row)
            height = max(t.box[3] - t.box[1] for t in row)
            joined = text + " " + next_text
            if (
                0 <= gap <= height * 1.5
                and "premier" in joined
                and any(ratio(word, "ecrites") >= 75 for word in joined.split())
                and partial_ratio("epreuves ecrites du premier groupe", joined) >= 80
            ):
                written.append(row + following)
    bac_anchor = "baccalaureat" in original_words and "general" in original_words
    oral_anchor = any("epreuves" in text and "orales" in text for _, text in texts)
    if not written:
        if oral_anchor:
            return BAC_GENERAL, [], ""
        return (BAC_GENERAL, [], "LOW_CONFIDENCE") if bac_anchor else (GENERIC, tokens, "")
    # A repeated written section is not resolved by selecting the first silently.
    if len(written) != 1:
        return BAC_GENERAL, [], "AMBIGUOUS_ROW"
    start = max(t.box[3] for t in written[0])
    endings = [
        min(t.box[1] for t in row)
        for row, text in texts
        if min(t.box[1] for t in row) > start
        and (
            ("epreuves" in text and ("oral" in text or "sport" in text))
            or "total des epreuves ecrites" in text
            or "total du" in text
            or "total du 1" in text
        )
    ]
    end = min(endings, default=1.0)
    selected = [t for t in tokens if t.box[1] >= start and t.box[3] <= end]
    return replace(BAC_GENERAL, bounds=(start, end)), selected, ""

# Stable BAC-general table geometry, measured relative to its outer border, not page X.
BAC_COLUMN_RATIOS = (0.0, 0.535, 0.645, 0.755, 0.89, 1.0)
