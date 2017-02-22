// Copyright 2016 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"hash/crc32"
	"strings"
	"time"

	pbinlog "github.com/cwen0/cdb-syncer/protocol"
	"github.com/go-sql-driver/mysql"
	"github.com/juju/errors"
	"github.com/ngaut/log"
	tddl "github.com/pingcap/tidb/ddl"
	"github.com/pingcap/tidb/infoschema"
	tmysql "github.com/pingcap/tidb/mysql"
	"github.com/pingcap/tidb/terror"
)

type job struct {
	tp    pbinlog.BinlogType
	sql   string
	args  []interface{}
	key   string
	retry bool
	pos   Position
}

func newJob(tp pbinlog.BinlogType, sql string, args []interface{}, key string, retry bool, pos Position) *job {
	return &job{tp: tp, sql: sql, args: args, key: key, retry: retry, pos: pos}
}

func genHashKey(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

func genPKey(rows []*pbinlog.Row) string {
	var values []string
	for _, row := range rows {
		values = append(values, row.GetColumnValue())
	}
	return strings.Join(values, ",")
}

func genInsertSQL(binlog *pbinlog.Binlog) (string, string, []interface{}, error) {
	var sql string
	var values []string
	sql += "replace into " + binlog.GetDbName() + "." + binlog.GetTableName() + "("
	rows := binlog.GetRows()
	for _, row := range rows {
		sql += row.GetColumnName() + ","
		values = append(values, row.GetColumnValue())
	}
	sql = sql[0:len(sql)-1] + ") values ("
	for _, _ = range rows {
		sql += "?,"
	}
	sql = sql[0:len(sql)-1] + ")"

	return sql, binlog.GetTableName() + genPKey(binlog.GetPrimaryKey()), stringToInterface(values), nil
}

func genUpdateSQL(binlog *pbinlog.Binlog) (string, string, []interface{}, error) {
	var sql string
	var values []string
	sql += "update " + binlog.GetDbName() + "." + binlog.GetTableName() + " set "
	rows := binlog.GetRows()
	for _, row := range rows {
		sql += row.GetColumnName() + "=?,"
		values = append(values, row.GetColumnValue())
	}
	sql = sql[0:len(sql)-1] + " where 1=1 "
	for _, row := range binlog.GetPrimaryKey() {
		sql += " and " + row.GetColumnName() + " = ? "
		values = append(values, row.GetColumnValue())
	}
	return sql, binlog.GetTableName() + genPKey(binlog.GetPrimaryKey()), stringToInterface(values), nil
}

func genDeleteSQL(binlog *pbinlog.Binlog) (string, string, []interface{}, error) {
	var sql string
	var values []string
	sql += "delete from " + binlog.GetDbName() + "." + binlog.GetTableName() + " where 1=1 "
	for _, row := range binlog.GetPrimaryKey() {
		sql += " and " + row.GetColumnName() + " = ? "
		values = append(values, row.GetColumnValue())
	}
	return sql, binlog.GetTableName() + genPKey(binlog.GetPrimaryKey()), stringToInterface(values), nil
}

func genDdlSQL(binlog *pbinlog.Binlog) (string, string, []interface{}, error) {
	var sql string
	if binlog.GetDbName() != "" {
		sql += "use " + binlog.GetDbName() + ";"
	}
	rows := binlog.GetRows()
	for _, row := range rows {
		sql += row.GetSql() + ";"
	}
	empty := make([]interface{}, 0)
	return sql, "", empty, nil
}

func ignoreDDLError(err error) bool {
	mysqlErr, ok := errors.Cause(err).(*mysql.MySQLError)
	if !ok {
		return false
	}

	errCode := terror.ErrCode(mysqlErr.Number)
	switch errCode {
	case infoschema.ErrDatabaseExists.Code(), infoschema.ErrDatabaseNotExists.Code(), infoschema.ErrDatabaseDropExists.Code(),
		infoschema.ErrTableExists.Code(), infoschema.ErrTableNotExists.Code(), infoschema.ErrTableDropExists.Code(),
		infoschema.ErrColumnExists.Code(), infoschema.ErrColumnNotExists.Code(),
		infoschema.ErrIndexExists.Code(), tddl.ErrCantDropFieldOrKey.Code():
		return true
	default:
		return false
	}
}

func isRetryableError(err error) bool {
	if err == driver.ErrBadConn {
		return true
	}
	var e error
	for {
		e = errors.Cause(err)
		if err == e {
			break
		}
		err = e
	}
	mysqlErr, ok := err.(*mysql.MySQLError)
	if ok {
		if mysqlErr.Number == tmysql.ErrUnknown {
			return true
		}
		return false
	}

	return true
}

func querySQL(db *sql.DB, query string) (*sql.Rows, error) {
	var (
		err  error
		rows *sql.Rows
	)

	for i := 0; i < maxRetryCount; i++ {
		if i > 0 {
			log.Warnf("query sql retry %d - %s", i, query)
			time.Sleep(retryTimeout)
		}

		log.Debugf("[query][sql]%s", query)

		rows, err = db.Query(query)
		if err != nil {
			if !isRetryableError(err) {
				return rows, errors.Trace(err)
			}
			log.Warnf("[query][sql]%s[error]%v", query, err)
			continue
		}

		return rows, nil
	}

	if err != nil {
		log.Errorf("query sql[%s] failed %v", query, errors.ErrorStack(err))
		return nil, errors.Trace(err)
	}

	return nil, errors.Errorf("query sql[%s] failed", query)
}

func executeSQL(db *sql.DB, sqls []string, args [][]interface{}, retry bool) error {
	if len(sqls) == 0 {
		return nil
	}

	var (
		err error
		txn *sql.Tx
	)

	retryCount := 1
	if retry {
		retryCount = maxRetryCount
	}

LOOP:
	for i := 0; i < retryCount; i++ {
		if i > 0 {
			log.Warnf("exec sql retry %d - %v - %v", i, sqls, args)
			time.Sleep(retryTimeout)
		}

		txn, err = db.Begin()
		if err != nil {
			log.Errorf("exec sqls[%v] begin failed %v", sqls, errors.ErrorStack(err))
			continue
		}

		for i := range sqls {
			log.Debugf("[exec][sql]%s[args]%v", sqls[i], args[i])

			_, err = txn.Exec(sqls[i], args[i]...)
			if err != nil {
				if !isRetryableError(err) {
					rerr := txn.Rollback()
					if rerr != nil {
						log.Errorf("[exec][sql]%s[args]%v[error]%v", sqls[i], args[i], rerr)
					}
					break LOOP
				}

				log.Warnf("[exec][sql]%s[args]%v[error]%v", sqls[i], args[i], err)
				rerr := txn.Rollback()
				if rerr != nil {
					log.Errorf("[exec][sql]%s[args]%v[error]%v", sqls[i], args[i], rerr)
				}
				continue LOOP
			}
		}
		err = txn.Commit()
		if err != nil {
			log.Errorf("exec sqls[%v] commit failed %v", sqls, errors.ErrorStack(err))
			continue
		}

		return nil
	}

	if err != nil {
		log.Errorf("exec sqls[%v] failed %v", sqls, errors.ErrorStack(err))
		return errors.Trace(err)
	}

	return errors.Errorf("exec sqls[%v] failed", sqls)
}

func createDB(cfg DBConfig) (*sql.DB, error) {
	dbDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8&interpolateParams=true", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	db, err := sql.Open("mysql", dbDSN)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return db, nil
}

func closeDB(db *sql.DB) error {
	if db == nil {
		return nil
	}

	return errors.Trace(db.Close())
}

func createDBs(cfg DBConfig, count int) ([]*sql.DB, error) {
	dbs := make([]*sql.DB, 0, count)
	for i := 0; i < count; i++ {
		db, err := createDB(cfg)
		if err != nil {
			return nil, errors.Trace(err)
		}

		dbs = append(dbs, db)
	}

	return dbs, nil
}

func closeDBs(dbs ...*sql.DB) {
	for _, db := range dbs {
		err := closeDB(db)
		if err != nil {
			log.Errorf("close db failed - %v", err)
		}
	}
}
