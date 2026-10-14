"""Exact row and table arithmetic of the BAC cell grid (CLAUDE.md #4, ADR 079-081).

Cells are read independently. The only value ever computed for a cell is the reconstruction of a
single unreadable cell per additive column from the Total row (RECONSTRUCTED_FROM_TOTAL); it counts
only once a check of its own row confirms it with cells read directly.

Acceptance applies to expected subjects only; the other rows only serve the totals:
- note read and Points = Note x Coeff on its row -> VALIDATED_BY_ROW (source DIRECT_CELL);
- note unreadable, Points read, table verified and every other row confirmed by its read note
                                                  -> VALIDATED_BY_TOTALS (source POINTS_TOTALS);
- note read but Points read != Note x Coeff       -> GRADE_POINTS_MISMATCH;
- anything else                                   -> NEEDS_REVIEW.
The coefficient is always its own cell: an integer 1..6 with Sur = 20 x Coeff.
"""

from decimal import Decimal
from fractions import Fraction
from math import floor

from ifri_ocr.extraction import number

READ = "READ"
RECONSTRUCTED = "RECONSTRUCTED_FROM_TOTAL"
GRADE, COEFFICIENT, POINTS, SCALE = range(4)
# Columns whose Total cell is a sum; the Total note cell is the displayed average.
ADDITIVE = {COEFFICIENT: "coefficient", POINTS: "points", SCALE: "scale"}
ACCEPTED = {"VALIDATED_BY_ROW", "VALIDATED_BY_TOTALS"}


def parse_numbers(raw):
    """raw = [note, coeff, points, sur] as read. A note outside [0, 20] is no reading."""
    values = [number(value) for value in raw]
    if values[GRADE] is not None and not 0 <= values[GRADE] <= 20:
        values[GRADE] = None
    return {
        "rawCells": list(raw),
        "parsed": [None if v is None else str(v) for v in values],
        "cellSources": [None if v is None else READ for v in values],
    }


def value(row, column):
    text = row["parsed"][column]
    return None if text is None else Fraction(Decimal(text))


def rebuilt(row, column):
    return row["cellSources"][column] == RECONSTRUCTED


def decimal_text(fraction):
    """Exact decimal string of a terminating fraction, else None; never rounded."""
    denominator, twos, fives = fraction.denominator, 0, 0
    while denominator % 2 == 0:
        denominator, twos = denominator // 2, twos + 1
    while denominator % 5 == 0:
        denominator, fives = denominator // 5, fives + 1
    places = max(twos, fives)
    if denominator != 1 or places > 32:
        return None
    scaled = fraction * 10**places
    return format(Decimal(scaled.numerator).scaleb(-places), "f")


def reconstruct(rows, total):
    """Total - sum of the others for the single unreadable cell of an additive column.

    Two unreadable cells or more in a column, an unreadable Total cell or an impossible result
    (negative, zero coefficient/scale) leave the column as read.
    """
    report = {}
    for column, name in ADDITIVE.items():
        missing = [index for index, row in enumerate(rows) if row["parsed"][column] is None]
        target = value(total, column)
        entry = {"unreadable": len(missing), "reconstructed": False}
        if len(missing) == 1 and target is not None:
            result = target - sum(
                value(row, column) for index, row in enumerate(rows) if index != missing[0]
            )
            text = decimal_text(result)
            if text is not None and (result > 0 or (column == POINTS and result == 0)):
                rows[missing[0]]["parsed"][column] = text
                rows[missing[0]]["cellSources"][column] = RECONSTRUCTED
                entry["reconstructed"] = True
        report[name] = entry
    return report


def coefficient_validated(row):
    """Integer 1..6 with Sur = 20 x Coeff; a reconstructed operand needs the other one read."""
    coef, scale = value(row, COEFFICIENT), value(row, SCALE)
    if coef is None or scale is None or coef.denominator != 1 or not 1 <= coef <= 6:
        return False
    return scale == 20 * coef and not (rebuilt(row, COEFFICIENT) and rebuilt(row, SCALE))


def points_check(row):
    """Points = Note x Coeff: True/False when all three are known, None otherwise."""
    grade, coef, points = value(row, GRADE), value(row, COEFFICIENT), value(row, POINTS)
    if grade is None or coef is None or points is None:
        return None
    if rebuilt(row, COEFFICIENT) and rebuilt(row, POINTS):
        return None
    return points == grade * coef


def exact_quotient(points, coef):
    """Points / Coeff only when it is a terminating decimal in [0, 20]."""
    if points is None or coef is None or coef <= 0:
        return None
    quotient = points / coef
    if not 0 <= quotient <= 20 or decimal_text(quotient) is None:
        return None
    return quotient


def truncated(fraction, places=2):
    return Fraction(floor(fraction * 10**places), 10**places)


def rounded_half_up(fraction, places=2):
    return Fraction(floor(fraction * 10**places + Fraction(1, 2)), 10**places)


def verify_table(rows, total):
    """Totals validate a derived note only when they independently check every Points cell.

    All rows enter the sums whatever their label. Besides the three exact sums, every row must
    have a validated coefficient and a plausible exact Points / Coeff, a reconstructed Points must
    be confirmed by its row, and no readable note may contradict its Points: any of these reveals
    a misread that the sums could hide. The displayed average, when readable, must be the
    truncation (observed on the Office du Bac transcript) or the rounding of Points / Coeff.
    """
    checks = {}
    sums = ((COEFFICIENT, "coefficientSum"), (POINTS, "pointsSum"), (SCALE, "scaleSum"))
    for column, name in sums:
        target, values = value(total, column), [value(row, column) for row in rows]
        known = bool(rows) and target is not None and None not in values
        checks[name] = known and sum(values) == target
    checks["coefficientsValidated"] = bool(rows) and all(coefficient_validated(r) for r in rows)
    checks["rebuiltPointsConfirmed"] = all(
        points_check(row) is True for row in rows if rebuilt(row, POINTS)
    )
    checks["readableNotesAgree"] = all(points_check(row) is not False for row in rows)
    checks["plausibleQuotients"] = all(
        exact_quotient(value(row, POINTS), value(row, COEFFICIENT)) is not None for row in rows
    )
    displayed, coef, points = value(total, GRADE), value(total, COEFFICIENT), value(total, POINTS)
    checks["displayedTotalNote"] = None
    if displayed is not None and coef and points is not None:
        average = points / coef
        checks["displayedTotalNote"] = displayed in (truncated(average), rounded_half_up(average))
    required = [name for name in checks if name != "displayedTotalNote"]
    valid = all(checks[name] is True for name in required)
    return checks, valid and checks["displayedTotalNote"] is not False


def isolated(rows, index):
    """The totals check the Points of `rows[index]` only if every other row's Points is
    confirmed by its own read note; two unconfirmed rows could hide compensating misreads."""
    return all(points_check(row) is True for other, row in enumerate(rows) if other != index)


def decide(rows, index, table_valid):
    """Decision for the row of an expected subject: (status, grade text, grade source)."""
    row = rows[index]
    if not (coefficient_validated(row) and row["cellSources"][COEFFICIENT] == READ):
        return "NEEDS_REVIEW", None, None
    if row["parsed"][GRADE] is not None:
        check = points_check(row)
        if check is True:
            return "VALIDATED_BY_ROW", row["parsed"][GRADE], "DIRECT_CELL"
        if check is False and row["cellSources"][POINTS] == READ:
            return "GRADE_POINTS_MISMATCH", None, None
        return "NEEDS_REVIEW", None, None
    # A reconstructed Points is never the only source of a returned note.
    if table_valid and row["cellSources"][POINTS] == READ and isolated(rows, index):
        derived = exact_quotient(value(row, POINTS), value(row, COEFFICIENT))
        if derived is not None:
            return "VALIDATED_BY_TOTALS", decimal_text(derived), "POINTS_TOTALS"
    return "NEEDS_REVIEW", None, None


def evaluate(rows, total, expected_rows):
    """Reconstruct, verify the table, then decide each expected row. Returns (checks, outcome).

    Every row is annotated for diagnostics; only `expected_rows` can produce an accepted value.
    """
    report = reconstruct(rows, total)
    checks, table_valid = verify_table(rows, total)
    checks["reconstruction"] = report
    checks["tableVerified"] = table_valid
    for row in rows:
        row["coefficientStatus"] = "VALIDATED" if coefficient_validated(row) else "NEEDS_REVIEW"
        row["rowProduct"] = points_check(row)
        row["gradeStatus"], row["gradeValue"], row["gradeSource"] = None, None, None
    statuses = []
    for index in expected_rows:
        status, grade, source = decide(rows, index, table_valid)
        rows[index].update(gradeStatus=status, gradeValue=grade, gradeSource=source)
        statuses.append(status)
    if "GRADE_POINTS_MISMATCH" in statuses:
        outcome = "GRADE_POINTS_MISMATCH"
    elif not statuses or any(status not in ACCEPTED for status in statuses):
        outcome = "NEEDS_REVIEW"
    else:
        outcome = ""
    return checks, outcome
