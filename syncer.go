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
	"sync"
	"time"

	pbinlog "github.com/cwen0/cdb-syncer/protocol"
	"github.com/juju/errors"
	"github.com/ngaut/log"
	"github.com/siddontang/go/sync2"
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
	syncer.ctx, syncer.cancel = context.WithCancel(context.Background())
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

func (s *Syncer) addCount(tp pbinlog.BinlogType, n int64) {
	switch tp {
	case pbinlog.BinlogType_INSERT:
		s.insertCount.Add(n)
	case pbinlog.BinlogType_UPDATE:
		s.updateCount.Add(n)
	case pbinlog.BinlogType_DELETE:
		s.deleteCount.Add(n)
	case pbinlog.BinlogType_DDL:
		s.ddlCount.Add(n)
	}

	s.count.Add(n)
}

func (s *Syncer) checkWait(job *job) bool {
	if job.tp == pbinlog.BinlogType_DDL {
		return true
	}

	if s.meta.Check() {
		return true
	}

	return false
}

func (s *Syncer) addJob(job *job) error {
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
	tpCnt := make(map[pbinlog.BinlogType]int64)

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

			if job.tp == pbinlog.BinlogType_DDL {
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
		BinlogNamePre: s.cfg.BinlogNamePre,
	}

	var err error

	s.syncer = NewBinlogSyncer(&cfg)

	s.toDBs, err = createDBs(s.cfg.To, s.cfg.WorkerCount+1)
	if err != nil {
		return errors.Trace(err)
	}

	//s.genRegexMap()
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
			time.Sleep(10 * time.Second)
			continue
		}

		for _, binlog := range binlogArg {
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

	closeDBs(s.toDBs...)

	if s.syncer != nil {
		s.syncer = nil
	}

	s.closed.Set(true)
}
