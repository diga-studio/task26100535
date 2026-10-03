package quotaledger

import "errors"

var (
	errUnknownUser   = errors.New("quotaledger: unknown user")
	errUnknownDept   = errors.New("quotaledger: unknown department")
	errUnknownTenant = errors.New("quotaledger: unknown tenant")
)
