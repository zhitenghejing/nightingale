//go:build !unix

package cmdb

import "errors"

func (s *ReportStore) Lock() (func(), error) {
	return nil, errors.New("report archive locking requires a Unix host")
}
