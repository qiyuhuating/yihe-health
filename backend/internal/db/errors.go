package db

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

func IsForeignKey(err error) bool {
	var constraint *mysql.MySQLError
	return errors.As(err, &constraint) && (constraint.Number == 1451 || constraint.Number == 1452)
}
