package source

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidSource     = errors.New("invalid source")
	ErrUnavailableSource = errors.New("source unavailable")
	ErrUnsupportedSource = errors.New("unsupported source")
)

type InvalidSourceError struct {
	ID string
}

func (e InvalidSourceError) Error() string {
	if e.ID == "" {
		return ErrInvalidSource.Error()
	}
	return fmt.Sprintf("%s: %s", ErrInvalidSource, e.ID)
}

func (e InvalidSourceError) Unwrap() error { return ErrInvalidSource }

type UnavailableSourceError struct {
	ID     SourceID
	Reason string
}

func (e UnavailableSourceError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("%s: %s", ErrUnavailableSource, e.ID)
	}
	return fmt.Sprintf("%s: %s: %s", ErrUnavailableSource, e.ID, e.Reason)
}

func (e UnavailableSourceError) Unwrap() error { return ErrUnavailableSource }

type UnsupportedSourceError struct {
	ID     string
	Reason string
}

func (e UnsupportedSourceError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("%s: %s", ErrUnsupportedSource, e.ID)
	}
	return fmt.Sprintf("%s: %s: %s", ErrUnsupportedSource, e.ID, e.Reason)
}

func (e UnsupportedSourceError) Unwrap() error { return ErrUnsupportedSource }
