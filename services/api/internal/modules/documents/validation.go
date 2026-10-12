package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"ifri-admissions/services/api/internal/platform/pdfcheck"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Upload struct {
	File     *os.File
	Filename string
	Size     int64
	Hash     string
}

func (u Upload) Close() { name := u.File.Name(); u.File.Close(); os.Remove(name) }
func Validate(ctx context.Context, body io.Reader, filename, contentType string, max int64, inspector pdfcheck.Inspector) (Upload, error) {
	invalid := issue(422, "PDF_INVALID", "Ce fichier ne semble pas être un PDF valide.")
	if strings.ToLower(filepath.Ext(filename)) != ".pdf" {
		return Upload{}, invalid
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil || media != "application/pdf" {
		return Upload{}, issue(415, "PDF_MIME_INVALID", "Choisissez un fichier au format PDF.")
	}
	name := strings.ReplaceAll(filename, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}
	if name == "" || !utf8.ValidString(name) {
		return Upload{}, invalid
	}
	file, err := os.CreateTemp("", "ifri-pdf-*.pdf")
	if err != nil {
		return Upload{}, err
	}
	ok := false
	defer func() {
		if !ok {
			file.Close()
			os.Remove(file.Name())
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(body, max+1))
	if err != nil {
		return Upload{}, issue(400, "UPLOAD_INTERRUPTED", "L’envoi a été interrompu. Réessayez.")
	}
	if size > max {
		return Upload{}, issue(413, "DOCUMENT_TOO_LARGE", "Ce fichier dépasse la taille maximale autorisée.")
	}
	if size == 0 {
		return Upload{}, invalid
	}
	file.Seek(0, io.SeekStart)
	header := make([]byte, 8)
	if _, err = io.ReadFull(file, header); err != nil || string(header[:5]) != "%PDF-" {
		return Upload{}, invalid
	}
	if err = inspector.Check(ctx, file.Name()); err != nil {
		if errors.Is(err, pdfcheck.ErrEncrypted) {
			return Upload{}, issue(422, "PDF_ENCRYPTED", "Le PDF est protégé. Fournissez une version sans mot de passe.")
		}
		if errors.Is(err, pdfcheck.ErrActive) {
			return Upload{}, issue(422, "PDF_ACTIVE_CONTENT", "Le PDF contient des éléments actifs ou des pièces jointes non autorisés.")
		}
		return Upload{}, invalid
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return Upload{}, err
	}
	ok = true
	return Upload{file, name, size, hex.EncodeToString(hash.Sum(nil))}, nil
}
