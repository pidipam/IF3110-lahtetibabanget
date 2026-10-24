package migration

import "errors"

var (
	ErrInvalidTransition = errors.New("perubahan status migration tidak diizinkan")
	ErrNotDraft          = errors.New("hanya migration berstatus draft yang dapat diubah atau dijalankan")
)

var transitions = map[Status][]Status{
	StatusDraft:   {StatusRunning},
	StatusRunning: {StatusSuccess, StatusFailed},
}

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusRunning, StatusSuccess, StatusFailed:
		return true
	}
	return false
}

func (s Status) Editable() bool { return s == StatusDraft }

func (s Status) Final() bool { return s == StatusSuccess || s == StatusFailed }

func (s Status) Label() string {
	switch s {
	case StatusDraft:
		return "Draft"
	case StatusRunning:
		return "Berjalan"
	case StatusSuccess:
		return "Berhasil"
	case StatusFailed:
		return "Gagal (rollback)"
	}
	return string(s)
}

func CanTransition(from, to Status) bool {
	for _, next := range transitions[from] {
		if next == to {
			return true
		}
	}
	return false
}
