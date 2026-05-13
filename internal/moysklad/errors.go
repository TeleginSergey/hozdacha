package moysklad

import "errors"

// ErrResyncRequired — дельта-фильтр недействителен (410 и т.п.), нужна полная выгрузка.
var ErrResyncRequired = errors.New("moysklad: full resync required")
