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
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	pbinlog "github.com/cwen0/cdb-syncer/protocol"
	"github.com/juju/errors"
	"github.com/ngaut/log"
	"github.com/siddontang/go/sync2"
	//"github.com/siddontang/go/sync2"
	"golang.org/x/net/context"
)

var (
	maxRetryCount = 100

	retryTimeout = 3 * time.Second
	waitTime     = 10 * time.Millisecond
	maxWaitTime  = 3 * time.Second
	eventTimeout = 3 * time.Second
	statusTime   = 30 * time.Second
)

// Syncer can sync your MySQL data into another MySQL database.
type Syncer struct {
	sync.Mutex

	cfg *Config

	meta Meta

	syncer *BinlogSyncer

	wg    sync.WaitGroup
	jobWg sync.WaitGroup

	tables map[string]*table

	toDBs []*sql.DB

	done chan struct{}
	jobs []chan *job

	closed sync2.AtomicBool

	start    time.Time
	lastTime time.Time

	ddlCount    sync2.AtomicInt64
	insertCount sync2.AtomicInt64
	updateCount sync2.AtomicInt64
	deleteCount sync2.AtomicInt64
	lastCount   sync2.AtomicInt64
	count       sync2.AtomicInt64

	ctx    context.Context
	cancel context.CancelFunc

	reMap map[string]*regexp.Regexp
}

// NewSyncer creates a new Syncer.
func NewSyncer(cfg *Config) *Syncer {
	syncer := new(Syncer)
	syncer.cfg = cfg
	syncer.meta = NewLocalMeta(cfg.Meta)
	syncer.closed.Set(false)
	syncer.lastCount.Set(0)
	syncer.count.Set(0)
	syncer.insertCount.Set(0)
	syncer.updateCount.Set(0)
	syncer.deleteCount.Set(0)
	syncer.done = make(chan struct{})
	syncer.jobs = newJobChans(cfg.WorkerCount)
	syncer.tables = make(map[string]*table)
	syncer.ctx, syncer.cancel = context.WithCancel(context.Background())
	syncer.reMap = make(map[string]*regexp.Regexp)
	return syncer
}

func newJobChans(count int) []chan *job {
	jobs := make([]chan *job, 0, count)
	for i := 0; i < count; i++ {
		jobs = append(jobs, make(chan *job, 1000))
	}

	return jobs
}

func closeJobChans(jobs []chan *job) {
	for _, ch := range jobs {
		close(ch)
	}
}

// Start starts syncer.
func (s *Syncer) Start() error {
	err := s.meta.Load()
	if err != nil {
		return errors.Trace(err)
	}

	s.wg.Add(1)

	err = s.run()
	if err != nil {
		return errors.Trace(err)
	}

	s.done <- struct{}{}

	return nil
}

func (s *Syncer) checkBinlogFormat() error {
	rows, err := s.fromDB.Query(`SHOW GLOBAL VARIABLES LIKE "binlog_format";`)
	if err != nil {
		return errors.Trace(err)
	}
	defer rows.Close()

	// Show an example.
	/*
		mysql> SHOW GLOBAL VARIABLES LIKE "binlog_format";
		+---------------+-------+
		| Variable_name | Value |
		+---------------+-------+
		| binlog_format | ROW   |
		+---------------+-------+
	*/
	for rows.Next() {
		var (
			variable string
			value    string
		)

		err = rows.Scan(&variable, &value)

		if err != nil {
			return errors.Trace(err)
		}

		if variable == "binlog_format" && value != "ROW" {
			log.Fatalf("We just support ROW event now")
		}

	}

	if rows.Err() != nil {
		return errors.Trace(rows.Err())
	}

	return nil
}

func (s *Syncer) clearTables() {
	s.tables = make(map[string]*table)
}

func (s *Syncer) getTableFromDB(db *sql.DB, schema string, name string) (*table, error) {
	table := &table{}
	table.schema = schema
	table.name = name

	err := s.getTableColumns(db, table)
	if err != nil {
		return nil, errors.Trace(err)
	}

	err = s.getTableIndex(db, table)
	if err != nil {
		return nil, errors.Trace(err)
	}

	if len(table.columns) == 0 {
		return nil, errors.Errorf("invalid table %s.%s", schema, name)
	}

	return table, nil
}

func (s *Syncer) getTableColumns(db *sql.DB, table *table) error {
	if table.schema == "" || table.name == "" {
		return errors.New("schema/table is empty")
	}

	query := fmt.Sprintf("show columns from `%s`.`%s`", table.schema, table.name)
	rows, err := querySQL(db, query)
	if err != nil {
		return errors.Trace(err)
	}
	defer rows.Close()

	rowColumns, err := rows.Columns()
	if err != nil {
		return errors.Trace(err)
	}

	// Show an example.
	/*
	   mysql> show columns from test.t;
	   +-------+---------+------+-----+---------+-------+
	   | Field | Type    | Null | Key | Default | Extra |
	   +-------+---------+------+-----+---------+-------+
	   | a     | int(11) | NO   | PRI | NULL    |       |
	   | b     | int(11) | NO   | PRI | NULL    |       |
	   | c     | int(11) | YES  | MUL | NULL    |       |
	   | d     | int(11) | YES  |     | NULL    |       |
	   +-------+---------+------+-----+---------+-------+
	*/

	idx := 0
	for rows.Next() {
		datas := make([]sql.RawBytes, len(rowColumns))
		values := make([]interface{}, len(rowColumns))

		for i := range values {
			values[i] = &datas[i]
		}

		err = rows.Scan(values...)
		if err != nil {
			return errors.Trace(err)
		}

		column := &column{}
		column.idx = idx
		column.name = string(datas[0])

		// Check whether column has unsigned flag.
		if strings.Contains(strings.ToLower(string(datas[1])), "unsigned") {
			column.unsigned = true
		}

		table.columns = append(table.columns, column)
		idx++
	}

	if rows.Err() != nil {
		return errors.Trace(rows.Err())
	}

	return nil
}

func (s *Syncer) getTableIndex(db *sql.DB, table *table) error {
	if table.schema == "" || table.name == "" {
		return errors.New("schema/table is empty")
	}

	query := fmt.Sprintf("show index from `%s`.`%s`", table.schema, table.name)
	rows, err := querySQL(db, query)
	if err != nil {
		return errors.Trace(err)
	}
	defer rows.Close()

	rowColumns, err := rows.Columns()
	if err != nil {
		return errors.Trace(err)
	}

	// Show an example.
	/*
		mysql> show index from test.t;
		+-------+------------+----------+--------------+-------------+-----------+-------------+----------+--------+------+------------+---------+---------------+
		| Table | Non_unique | Key_name | Seq_in_index | Column_name | Collation | Cardinality | Sub_part | Packed | Null | Index_type | Comment | Index_comment |
		+-------+------------+----------+--------------+-------------+-----------+-------------+----------+--------+------+------------+---------+---------------+
		| t     |          0 | PRIMARY  |            1 | a           | A         |           0 |     NULL | NULL   |      | BTREE      |         |               |
		| t     |          0 | PRIMARY  |            2 | b           | A         |           0 |     NULL | NULL   |      | BTREE      |         |               |
		| t     |          0 | ucd      |            1 | c           | A         |           0 |     NULL | NULL   | YES  | BTREE      |         |               |
		| t     |          0 | ucd      |            2 | d           | A         |           0 |     NULL | NULL   | YES  | BTREE      |         |               |
		+-------+------------+----------+--------------+-------------+-----------+-------------+----------+--------+------+------------+---------+---------------+
	*/
	var keyName string
	var columns []string
	for rows.Next() {
		datas := make([]sql.RawBytes, len(rowColumns))
		values := make([]interface{}, len(rowColumns))

		for i := range values {
			values[i] = &datas[i]
		}

		err = rows.Scan(values...)
		if err != nil {
			return errors.Trace(err)
		}

		nonUnique := string(datas[1])
		if nonUnique == "0" {
			if keyName == "" {
				keyName = string(datas[2])
			} else {
				if keyName != string(datas[2]) {
					break
				}
			}

			columns = append(columns, string(datas[4]))
		}
	}

	if rows.Err() != nil {
		return errors.Trace(rows.Err())
	}

	table.indexColumns = findColumns(table.columns, columns)
	return nil
}

func (s *Syncer) getTable(schema string, table string) (*table, error) {
	key := fmt.Sprintf("%s.%s", schema, table)

	value, ok := s.tables[key]
	if ok {
		return value, nil
	}

	db := s.toDBs[len(s.toDBs)-1]
	t, err := s.getTableFromDB(db, schema, table)
	if err != nil {
		return nil, errors.Trace(err)
	}

	s.tables[key] = t
	return t, nil
}

func (s *Syncer) addCount(tp opType, n int64) {
	switch tp {
	case insert:
		s.insertCount.Add(n)
	case update:
		s.updateCount.Add(n)
	case del:
		s.deleteCount.Add(n)
	case ddl:
		s.ddlCount.Add(n)
	}

	s.count.Add(n)
}

func (s *Syncer) checkWait(job *job) bool {
	if job.tp == ddl {
		return true
	}

	if s.meta.Check() {
		return true
	}

	return false
}

func (s *Syncer) addJob(job *job) error {
	if job.tp == xid {
		s.meta.Save(job.pos, false)
		return nil
	}

	s.jobWg.Add(1)

	log.Debugf("add job [sql]%s; [position]%v", job.sql, job.pos)
	idx := int(genHashKey(job.key)) % s.cfg.WorkerCount
	s.jobs[idx] <- job

	wait := s.checkWait(job)
	if wait {
		s.jobWg.Wait()

		err := s.meta.Save(job.pos, true)
		if err != nil {
			return errors.Trace(err)
		}
	}

	return nil
}

func (s *Syncer) sync(db *sql.DB, jobChan chan *job) {
	defer s.wg.Done()

	idx := 0
	count := s.cfg.Batch
	sqls := make([]string, 0, count)
	args := make([][]interface{}, 0, count)
	lastSyncTime := time.Now()
	tpCnt := make(map[opType]int64)

	clearF := func() {
		for i := 0; i < idx; i++ {
			s.jobWg.Done()
		}

		idx = 0
		sqls = sqls[0:0]
		args = args[0:0]
		lastSyncTime = time.Now()
		for tpName, v := range tpCnt {
			s.addCount(tpName, v)
			tpCnt[tpName] = 0
		}
	}

	var err error
	for {
		select {
		case job, ok := <-jobChan:
			if !ok {
				return
			}
			idx++

			if job.tp == ddl {
				err = executeSQL(db, sqls, args, true)
				if err != nil {
					log.Fatalf(errors.ErrorStack(err))
				}

				err = executeSQL(db, []string{job.sql}, [][]interface{}{job.args}, false)
				if err != nil {
					if !ignoreDDLError(err) {
						log.Fatalf(errors.ErrorStack(err))
					} else {
						log.Warnf("[ignore ddl error][sql]%s[args]%v[error]%v", job.sql, job.args, err)
					}
				}

				tpCnt[job.tp]++
				clearF()

			} else {
				sqls = append(sqls, job.sql)
				args = append(args, job.args)
				tpCnt[job.tp]++
			}

			if idx >= count {
				err = executeSQL(db, sqls, args, true)
				if err != nil {
					log.Fatalf(errors.ErrorStack(err))
				}
				clearF()
			}

		default:
			now := time.Now()
			if now.Sub(lastSyncTime) >= maxWaitTime {
				err = executeSQL(db, sqls, args, true)
				if err != nil {
					log.Fatalf(errors.ErrorStack(err))
				}
				clearF()
			}

			time.Sleep(waitTime)
		}
	}
}

func (s *Syncer) run() error {
	defer s.wg.Done()
	cfg := BinlogSyncerConfig{
		BinlogPath:    s.cfg.BinlogPath,
		BinlogFilePre: s.cfg.BinlogFilePre,
	}

	var err error

	s.syncer = newBinlogSyncer(&cfg)

	s.toDBs, err = clearTables(s.cfg.To, s.cfg.WorkerCount+1)
	if err != nil {
		return errors.Trace(err)
	}

	s.genRegexMap()

	s.start = time.Now()
	s.lastTime = s.start
	s.wg.Add(s.cfg.WorkerCount)
	for i := 0; i < s.cfg.WorkerCount; i++ {
		go s.sync(s.toDBs[i], s.jobs[i])
	}

	s.wg.Add(1)
	go s.printStatus()

	pos := s.meta.Pos()
	s.syncer.SetCurrentPos(pos)
	for {
		binlogArg, err := s.syncer.GetBinlogs()
		if err != nil {
			return errors.Trace(err)
		}

		if len(binlogArg) == 0 {
			time.Sleep(2 * time.Minute)
			continue
		}

		for binlog := range binlogArg {
			switch binlog.GetType() {
			case pbinlog.BinlogType_INSERT:
				sql, pKey, args, err := genInsertSQL(binlog)
				if err != nil {
					return errors.Errorf("gen insert sql failed: %v , DB: %s, table: %s", err, binlog.GetDbName(), binlog.GetTableName())
				}
				job := newJob(pbinlog.BinlogType_INSERT, sql, args, pKey, true, pos)
				err = s.addJob(job)
				if err != nil {
					return errors.Trace(err)
				}
			case pbinlog.BinlogType_UPDATE:
				sql, pKey, args, err := genUpdateSQL(binlog)
				if err != nil {
					return errors.Errorf("gen update sql failed: %v , DB: %s, table: %s", err, binlog.GetDbName(), binlog.GetTableName())
				}
				job := newJob(pbinlog.BinlogType_UPDATE, sql, args, pKey, true, pos)
				err = s.addJob(job)
				if err != nil {
					return errors.Trace(err)
				}
			case pbinlog.BinlogType_DELETE:
				sql, pKey, args, err := genDeleteSQL(binlog)
				if err != nil {
					return errors.Errorf("gen delete sql failed: %v , DB: %s, table: %s", err, binlog.GetDbName(), binlog.GetTableName())
				}
				job := newJob(pbinlog.BinlogType_DELETE, sql, args, pKey, true, pos)
				err = s.addJob(job)
				if err != nil {
					return errors.Trace(err)
				}
			case pbinlog.BinlogType_DDL:
				sql, pKey, args, err := genDdlSQL(binlog)
				if err != nil {
					return errors.Errorf("gen ddl sql failed: %v", err)
				}
				job := newJob(pbinlog.BinlogType_DDL, sql, args, pKey, true, pos)
				err = s.addJob(job)
				if err != nil {
					return errors.Trace(err)
				}
			default:
				break
			}
		}
	}
}

func (s *Syncer) genRegexMap() {
	for _, db := range s.cfg.DoDBs {
		if db[0] != '~' {
			continue
		}
		if _, ok := s.reMap[db]; !ok {
			s.reMap[db] = regexp.MustCompile(db[1:])
		}
	}

	for _, db := range s.cfg.IgnoreDBs {
		if db[0] != '~' {
			continue
		}
		if _, ok := s.reMap[db]; !ok {
			s.reMap[db] = regexp.MustCompile(db[1:])
		}
	}

	for _, tb := range s.cfg.DoTables {
		if tb.Name[0] == '~' {
			if _, ok := s.reMap[tb.Name]; !ok {
				s.reMap[tb.Name] = regexp.MustCompile(tb.Name[1:])
			}
		}
		if tb.Schema[0] == '~' {
			if _, ok := s.reMap[tb.Schema]; !ok {
				s.reMap[tb.Schema] = regexp.MustCompile(tb.Schema[1:])
			}
		}
	}

	for _, tb := range s.cfg.IgnoreTables {
		if tb.Name[0] == '~' {
			if _, ok := s.reMap[tb.Name]; !ok {
				s.reMap[tb.Name] = regexp.MustCompile(tb.Name[1:])
			}
		}
		if tb.Schema[0] == '~' {
			if _, ok := s.reMap[tb.Schema]; !ok {
				s.reMap[tb.Schema] = regexp.MustCompile(tb.Schema[1:])
			}
		}
	}
}

func (s *Syncer) printStatus() {
	defer s.wg.Done()

	timer := time.NewTicker(statusTime)
	defer timer.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			now := time.Now()
			seconds := now.Unix() - s.lastTime.Unix()
			totalSeconds := now.Unix() - s.start.Unix()
			last := s.lastCount.Get()
			total := s.count.Get()

			tps, totalTps := int64(0), int64(0)
			if seconds > 0 {
				tps = (total - last) / seconds
				totalTps = total / totalSeconds
			}

			log.Infof("[syncer]total events = %d, insert = %d, update = %d, delete = %d, total tps = %d, recent tps = %d, %s.",
				total, s.insertCount.Get(), s.updateCount.Get(), s.deleteCount.Get(), totalTps, tps, s.meta)

			s.lastCount.Set(total)
			s.lastTime = time.Now()
		}
	}
}

func (s *Syncer) isClosed() bool {
	return s.closed.Get()
}

// Close closes syncer.
func (s *Syncer) Close() {
	s.Lock()
	defer s.Unlock()

	if s.isClosed() {
		return
	}

	s.cancel()

	<-s.done

	closeJobChans(s.jobs)

	s.wg.Wait()

	closeDBs(s.fromDB)
	closeDBs(s.toDBs...)

	if s.syncer != nil {
		s.syncer.Close()
		s.syncer = nil
	}

	s.closed.Set(true)
}
