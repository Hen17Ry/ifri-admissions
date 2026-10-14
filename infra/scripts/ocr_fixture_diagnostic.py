"""Inspect spatial tokens of one fixed synthetic fixture; never accepts real PDFs."""

import json
from pathlib import Path
from tempfile import TemporaryDirectory

import pymupdf
from ifri_ocr.extraction import Tesseract, lines

fixture = Path("/artifacts/GL-BAC_C-scan.pdf")
with TemporaryDirectory() as directory, pymupdf.open(fixture) as document:
    image = Path(directory) / "synthetic.png"
    pixmap = document[0].get_pixmap(dpi=200, colorspace=pymupdf.csGRAY)
    pixmap.save(image)
    tokens = Tesseract().extract(image, pixmap.width, pixmap.height, 30)
    for row in lines(tokens):
        print(
            json.dumps(
                [
                    {
                        "text": token.text,
                        "box": token.box,
                        "confidence": token.confidence,
                        "line": token.line_id,
                    }
                    for token in row
                ],
                ensure_ascii=False,
            )
        )
