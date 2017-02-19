package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	pbinlog "github.com/cwen0/cdb-syncer/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/juju/errors"
	"github.com/ngaut/log"
)

type BinlogSyncer struct {
	cfg        *BinlogSyncerConfig
	currentPos Position
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
	return b
}

func (b *BinlogSyncer) SetCurrentPos(pos Position) {
	if pos.BinlogName != "" {
		b.currentPos.BinlogName = pos.BinlogName
	}
	b.currentPos.Pos = pos.Pos
}

func (b *BinlogSyncer) GetBinlogs() ([]pbinlog.Binlog, error) {
	var binlogAry []pbinlog.Binlog
	var count int = 0
	var err error
Loop:
	var file *os.File
	for {
		file, err = os.Open(b.cfg.BinlogPath + "/" + b.currentPos.BinlogName)
		if err != nil {
			return binlogAry, errors.Trace(err)
		}
		defer file.Close()

		fileInfo, err := file.Stat()
		if err != nil {
			return binlogAry, errors.Trace(err)
		}

		if int64(b.currentPos.Pos) == fileInfo.Size() {
			isExist, nextBinlogName, err := b.isExistNextBinlogName()
			if err != nil {
				return binlogAry, errors.Trace(err)
			}
			if !isExist {
				return binlogAry, nil
			}
			file.Close()
			b.currentPos.BinlogName = nextBinlogName
			b.currentPos.Pos = 0
			continue
		} else if int64(b.currentPos.Pos) > fileInfo.Size() {
			return binlogAry, errors.Errorf("Read binlogfile %s offset %d error", b.currentPos.BinlogName, b.currentPos.Pos)
		}
		break
	}

	for i := count; i < 100; i++ {
		sb := make([]byte, 4)
		offset := b.currentPos.Pos
		var fileIsEnd bool = false
		var n int
		n, err = file.ReadAt(sb, int64(offset))
		if err != nil || n != 4 {
			return binlogAry, errors.Trace(err)
		}
		var s int
		buf := bytes.NewReader(sb)
		err = binary.Read(buf, binary.LittleEndian, &s)
		data := make([]byte, s)
		n, err = file.ReadAt(data, int64(offset+4))
		if n != s {
			return binlogAry, errors.Trace(err)
		}
		if err != nil {
			if err != io.EOF {
				return binlogAry, errors.Trace(err)
			}
			fileIsEnd = true
		}
		// in := bytes.NewReader(data)
		binlog := pbinlog.Binlog{}
		if err = proto.Unmarshal(data, &binlog); err != nil {
			return binlogAry, errors.Trace(err)
		}
		count++
		b.currentPos.Pos = offset + 4 + uint64(s)
		binlogAry = append(binlogAry, binlog)
		if fileIsEnd {
			isExist, nextBinlogName, err := b.isExistNextBinlogName()
			if err != nil {
				return binlogAry, errors.Trace(err)
			}
			if !isExist {
				return binlogAry, nil
			}
			file.Close()
			b.currentPos.BinlogName = nextBinlogName
			b.currentPos.Pos = 0
			goto Loop
		}
	}
	return binlogAry, nil
}

func (b *BinlogSyncer) isExistNextBinlogName() (bool, string, error) {
	numStr := strings.TrimPrefix(b.currentPos.BinlogName, b.cfg.BinlogNamePre)
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return false, "", errors.Trace(err)
	}
	nextBinlogName := b.cfg.BinlogNamePre + fmt.Sprint("%08d", num+1)
	if _, err = os.Stat(b.cfg.BinlogPath + "/" + nextBinlogName); os.IsNotExist(err) {
		return false, "", nil
	}
	return true, nextBinlogName, nil
}