// generated by stringer -type=DBErrorType; DO NOT EDIT

package database

import "fmt"

const _DBErrorType_name = "INTERNALVALIDATION_FAILUREENTITY_NOT_FOUND"

var _DBErrorType_index = [...]uint8{0, 8, 26, 42}

func (i DBErrorType) String() string {
	if i < 0 || i >= DBErrorType(len(_DBErrorType_index)-1) {
		return fmt.Sprintf("DBErrorType(%d)", i)
	}
	return _DBErrorType_name[_DBErrorType_index[i]:_DBErrorType_index[i+1]]
}
