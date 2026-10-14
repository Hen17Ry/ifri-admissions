"""Expected subject -> table row attribution on a closed documentary vocabulary.

The vocabulary describes subjects printed in BAC general written tables (a layout fact to extend
from the corpus), never the admission rules: expected subjects still come from the job snapshot.
Scores form a matrix (expected subjects x rows); a one-to-one assignment maximises the total
score, then each pair must clear an absolute floor and two margins, otherwise AMBIGUOUS_ROW.
"""

from rapidfuzz.fuzz import ratio

from ifri_ocr.extraction import normalize

BAC_VOCABULARY = (
    "ANGLAIS",
    "FRANCAIS",
    "MATHEMATIQUES",
    "HISTOIRE ET GEOGRAPHIE",
    "PHILOSOPHIE",
    "PHYSIQUE CHIMIE ET TECHNOLOGIE",
    "SCIENCES DE LA VIE ET DE LA TERRE",
    "SCIENCES PHYSIQUES",
    "ESPAGNOL",
    "ALLEMAND",
    "ECONOMIE",
    "ETUDE DE FABRICATION",
    "TECHNOLOGIE",
    "EDUCATION PHYSIQUE ET SPORTIVE",
)
# Canonical admission subject codes printed under these documentary labels.
SUBJECT_LABELS = {
    "MATH": "MATHEMATIQUES",
    "FRENCH": "FRANCAIS",
    "ENGLISH": "ANGLAIS",
    "MANUFACTURING_STUDY": "ETUDE DE FABRICATION",
}
MIN_SCORE = 0.85
MIN_MARGIN = 0.08
TOTAL_SCORE = 0.8
# Letter/digit confusions only; applied identically to every vocabulary word.
_CONFUSABLES = str.maketrans({"0": "o", "1": "i", "5": "s"})


def clean(text):
    return normalize(text).translate(_CONFUSABLES)


def word_scores(texts):
    """Best score of each vocabulary word over the label variants of one row."""
    variants = [clean(text) for text in texts if text and clean(text)]
    return {
        word: max((ratio(variant, normalize(word)) / 100 for variant in variants), default=0.0)
        for word in BAC_VOCABULARY
    }


def total_score(texts):
    variants = [clean(text) for text in texts if text and clean(text)]
    return max((ratio(variant.split()[0], "total") / 100 for variant in variants), default=0.0)


def vocabulary_word(subject):
    """Documentary label of an expected subject, by code first, then by unique close name."""
    if subject.get("code") in SUBJECT_LABELS:
        return SUBJECT_LABELS[subject["code"]]
    if subject.get("id") in SUBJECT_LABELS:
        return SUBJECT_LABELS[subject["id"]]
    name = normalize(subject.get("name", ""))
    close = [word for word in BAC_VOCABULARY if ratio(name, normalize(word)) / 100 >= 0.9]
    return close[0] if len(close) == 1 else None


def hungarian(cost):
    """Minimum-cost one-to-one assignment for n rows <= m columns (Kuhn-Munkres, O(n^2 m))."""
    n, m = len(cost), len(cost[0]) if cost else 0
    if n == 0 or n > m:
        return []
    infinity = float("inf")
    u, v = [0.0] * (n + 1), [0.0] * (m + 1)
    match, way = [0] * (m + 1), [0] * (m + 1)
    for i in range(1, n + 1):
        match[0], j0 = i, 0
        minimum, used = [infinity] * (m + 1), [False] * (m + 1)
        while True:
            used[j0] = True
            i0, delta, j1 = match[j0], infinity, 0
            for j in range(1, m + 1):
                if not used[j]:
                    current = cost[i0 - 1][j - 1] - u[i0] - v[j]
                    if current < minimum[j]:
                        minimum[j], way[j] = current, j0
                    if minimum[j] < delta:
                        delta, j1 = minimum[j], j
            for j in range(m + 1):
                if used[j]:
                    u[match[j]] += delta
                    v[j] -= delta
                else:
                    minimum[j] -= delta
            j0 = j1
            if match[j0] == 0:
                break
        while j0:
            j1 = way[j0]
            match[j0] = match[j1]
            j0 = j1
    return sorted((match[j] - 1, j - 1) for j in range(1, m + 1) if match[j])


def assign(expected, row_scores):
    """Return ({subject id: row index}, issue, diagnostics). Unreadable rows simply score low."""
    words = [vocabulary_word(subject) for subject in expected]
    diagnostics = {"subjects": []}
    if any(word is None for word in words):
        return {}, "SUBJECT_NOT_FOUND", diagnostics
    if len(row_scores) < len(expected):
        return {}, "NEEDS_REVIEW", diagnostics
    matrix = [[scores[word] for scores in row_scores] for word in words]
    pairs = hungarian([[1 - score for score in line] for line in matrix])
    assignment, issue = {}, ""
    for subject_index, row in pairs:
        subject, word = expected[subject_index], words[subject_index]
        score = matrix[subject_index][row]
        other_rows = max(
            (s for index, s in enumerate(matrix[subject_index]) if index != row), default=0.0
        )
        other_words = max(
            (s for candidate, s in row_scores[row].items() if candidate != word), default=0.0
        )
        diagnostics["subjects"].append(
            {
                "subject": subject["id"],
                "row": row,
                "score": round(score, 3),
                "secondRowScore": round(other_rows, 3),
                "bestOtherWordScore": round(other_words, 3),
            }
        )
        if score < MIN_SCORE:
            issue = issue or "NEEDS_REVIEW"
        elif score - other_rows < MIN_MARGIN or score - other_words < MIN_MARGIN:
            issue = "AMBIGUOUS_ROW"
        assignment[subject["id"]] = row
    return ({} if issue else assignment), issue, diagnostics
