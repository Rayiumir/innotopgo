package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func Connect(mysql_uri string) (*sql.DB, error) {
	db, err := sql.Open("mysql", mysql_uri)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func QueryTimeout(ctx context.Context, db *sql.DB, stmt string) (*sql.Rows, error) {
	queryctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := db.QueryContext(queryctx, stmt)

	//rows, err := db.Query(stmt)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func Query(db *sql.DB, stmt string) (*sql.Rows, error) {
	rows, err := db.Query(stmt)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func RunQuery(db *sql.DB, stmt string) error {
	_, err := db.Exec(stmt)
	if err != nil {
		return err
	}
	return nil
}

func GetData(rows *sql.Rows) ([]string, [][]string, error) {
	var result [][]string
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, err
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	vals := make([]interface{}, len(cols))
	for i, _ := range cols {
		vals[i] = new(sql.RawBytes)
	}
	for rows.Next() {
		err = rows.Scan(vals...)
		if err != nil {
			return nil, nil, err
		}
		var resultRow []string
		for i, col := range vals {
			var value string
			if col == nil {
				value = "NULL"
			} else {
				switch colTypes[i].DatabaseTypeName() {
				case "VARCHAR", "CHAR", "TEXT":
					value = fmt.Sprintf("%s", col)
				case "BIGINT":
					value = fmt.Sprintf("%s", col)
				case "INT":
					//value = fmt.Sprintf("%d", col)
					value = fmt.Sprintf("%s", col)
				case "DECIMAL":
					value = fmt.Sprintf("%s", col)
				default:
					value = fmt.Sprintf("%s", col)
				}
			}
			value = strings.Replace(value, "&", "", 1)
			resultRow = append(resultRow, value)
		}
		result = append(result, resultRow)
	}
	return cols, result, nil
}

func GetServerInfo(mydb *sql.DB) ([]string, [][]string, error) {
	stmt := `select @@version_comment, @@version, @@hostname, @@port`
	rows, err := Query(mydb, stmt)
	if err != nil {
		return nil, nil, err
	}
	cols, data, err := GetData(rows)
	if err != nil {
		return nil, nil, err
	}

	return cols, data, nil
}
