package model

import "errors"

var (
	ErrValidation = errors.New("validation")
	ErrNotFound   = errors.New("not found")
	ErrBusy       = errors.New("busy")
)
