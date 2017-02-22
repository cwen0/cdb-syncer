package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	pbinlog "github.com/cwen0/cdb-syncer/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/juju/errors"
	"github.com/ngaut/log"
)

type BinlogSyncer struct {
	cfg        *BinlogSyncerConfig
	currentPos Position
	ch         chan *pbinlog.Binlog
	ech        chan error
	err        error
}

type BinlogSyncerConfig struct {
	BinlogPath    string
	BinlogNamePre string
}

func NewBinlogSyncer(cfg *BinlogSyncerConfig) *BinlogSyncer {
	log.Infof("create BinlogSyncer with config %v", cfg)
	b := new(BinlogSyncer)
	b.cfg = cfg
	b.currentPos = Position{
		BinlogName: cfg.BinlogNamePre + string(fmt.Sprintf("%08d", 1)),
		Pos:        uint64(1),
	}
	b.ch = make(chan *pbinlog.Binlog, 10240)
	b.ech = make(chan error, 4)
	return b
}

func (b *BinlogSyncer) Start(pos Position) {
	b.setCurrentPos(pos)
	go b.readFromFile()
}

func (b *BinlogSyncer) setCurrentPos(pos Position) {
	if pos.BinlogName != "" {
		b.currentPos.BinlogName = pos.BinlogName
	}
	b.currentPos.Pos = pos.Pos
}

func (b *BinlogSyncer) GetBinlogs(ctx context.Context) (*pbinlog.Binlog, Position, error) {
	if b.err != nil {
		return nil, b.currentPos, errors.Trace(b.err)
	}
	select {
	case c := <-b.ch:
		return c, b.currentPos, nil
	case b.err = <-b.ech:
		return nil, b.currentPos, errors.Trace(b.err)
	case <-ctx.Done():
		return nil, b.currentPos, ctx.Err()
	}
}

func (b *BinlogSyncer) readFromFile() {
	var file *os.File
	var err error
Loop:
	file, err = os.Open(b.cfg.BinlogPath + "/" + b.currentPos.BinlogName)
	if err != nil {
		b.ech <- errors.Trace(err)
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		b.ech <- errors.Trace(err)
		return
	}
	for {
		if int64(b.currentPos.Pos) == fileInfo.Size() {
			isExist, nextBinlogName, err := b.isExistNextBinlogName()
			if err != nil {
				b.ech <- errors.Trace(err)
				return
			}
			if !isExist {
				time.Sleep(10 * time.Second)
				continue
			}
			file.Close()
			b.currentPos.BinlogName = nextBinlogName
			b.currentPos.Pos = 0
			goto Loop
		} else if int64(b.currentPos.Pos) > fileInfo.Size() {
			b.ech <- errors.Errorf("Read binlogfile %s offset %d error", b.currentPos.BinlogName, b.currentPos.Pos)
			return
		}
		break
	}

	for {
		sb := make([]byte, 8)
		offset := b.currentPos.Pos
		var n int
		n, err = file.ReadAt(sb, int64(offset))
		if n == 0 {
			time.Sleep(10 * time.Second)
			goto Loop
		}
		if err != nil || n != 8 {
			b.ech <- errors.Trace(err)
			return
		}
		var s int64
		buf := bytes.NewReader(sb)
		err := binary.Read(buf, binary.BigEndian, &s)
		if err != nil {
			b.ech <- errors.Trace(err)
			return
		}
		data := make([]byte, s)
		n, err = file.ReadAt(data, int64(offset+8))
		if n != int(s) {
			b.ech <- errors.Trace(err)
			return
		}
		if err != nil {
			if err != io.EOF {
				b.ech <- errors.Trace(err)
				return
			}
		}
		binlog := &pbinlog.Binlog{}
		if err = proto.Unmarshal(data, binlog); err != nil {
			b.ech <- errors.Trace(err)
			return
		}
		b.currentPos.Pos = offset + 8 + uint64(s)
		b.ch <- binlog
	}
	return
}

func (b *BinlogSyncer) isExistNextBinlogName() (bool, string, error) {
	numStr := strings.TrimPrefix(b.currentPos.BinlogName, b.cfg.BinlogNamePre)
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return false, "", errors.Trace(err)
	}
	nextBinlogName := b.cfg.BinlogNamePre + fmt.Sprintf("%08d", num+1)
	if _, err = os.Stat(b.cfg.BinlogPath + "/" + nextBinlogName); os.IsNotExist(err) {
		return false, "", nil
	}
	return true, nextBinlogName, nil
}
