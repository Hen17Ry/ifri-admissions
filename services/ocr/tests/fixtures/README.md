# Synthetic OCR fixtures

Generate these PDFs with the opt-in Compose `ocr-tests` service after migrating and seeding DEV_LOCAL. The generator reads expected subjects from the real Go reference API for the six required qualification/program pairs. Numerical values are written into synthetic PDFs only; they are not worker defaults or admission rules.

Six native PDFs and six scanned PDFs exercise the matrix. Additional GL/DT_DWM fixtures cover accents, missing subjects/coefficients, ambiguous rows, a different layout, slight skew, wrong subjects and degraded scans. `benchmark.json` records measured status and duration; inspect it before updating OCR_BENCHMARK. No personal information or real transcript belongs here.
