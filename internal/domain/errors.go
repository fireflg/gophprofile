package domain

import "errors"

var (
	// ErrAvatarNotFound — аватарка отсутствует или удалена (404).
	ErrAvatarNotFound = errors.New("avatar not found")
	// ErrSizeNotFound — запрошенный размер миниатюры не существует (404).
	ErrSizeNotFound = errors.New("requested size not found")
	// ErrThumbnailNotReady — миниатюры ещё обрабатываются воркером (409).
	ErrThumbnailNotReady = errors.New("thumbnail is not ready yet")
	// ErrForbidden — попытка изменить чужую аватарку (403).
	ErrForbidden = errors.New("forbidden")
	// ErrUserIDRequired — не передан заголовок X-User-ID (401).
	ErrUserIDRequired = errors.New("X-User-ID header is required")
	// ErrInvalidAvatarID — идентификатор не является UUID (400).
	ErrInvalidAvatarID = errors.New("invalid avatar id")
	// ErrUnsupportedFormat — MIME-тип вне белого списка (400).
	ErrUnsupportedFormat = errors.New("invalid file format")
	// ErrUnsupportedTargetFormat — конвертация в запрошенный формат не поддерживается (400).
	ErrUnsupportedTargetFormat = errors.New("unsupported target format")
	// ErrFileTooLarge — файл превышает лимит (413).
	ErrFileTooLarge = errors.New("file too large")
	// ErrEmptyFile — пустое тело файла (400).
	ErrEmptyFile = errors.New("file is empty")
	// ErrFileRequired — в multipart-форме нет поля file (400).
	ErrFileRequired = errors.New("file is required")
)
