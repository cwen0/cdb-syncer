// Code generated by protoc-gen-go.
// source: binlog.proto
// DO NOT EDIT!

/*
Package protocol is a generated protocol buffer package.

It is generated from these files:
	binlog.proto

It has these top-level messages:
	Binlog
	Row
	Pos
*/
package protocol

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type BinlogType int32

const (
	BinlogType_INSERT BinlogType = 0
	BinlogType_UPDATE BinlogType = 1
	BinlogType_DELETE BinlogType = 2
	BinlogType_DDL    BinlogType = 3
)

var BinlogType_name = map[int32]string{
	0: "INSERT",
	1: "UPDATE",
	2: "DELETE",
	3: "DDL",
}
var BinlogType_value = map[string]int32{
	"INSERT": 0,
	"UPDATE": 1,
	"DELETE": 2,
	"DDL":    3,
}

func (x BinlogType) Enum() *BinlogType {
	p := new(BinlogType)
	*p = x
	return p
}
func (x BinlogType) String() string {
	return proto.EnumName(BinlogType_name, int32(x))
}
func (x *BinlogType) UnmarshalJSON(data []byte) error {
	value, err := proto.UnmarshalJSONEnum(BinlogType_value, data, "BinlogType")
	if err != nil {
		return err
	}
	*x = BinlogType(value)
	return nil
}
func (BinlogType) EnumDescriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

type Binlog struct {
	Postion          *Pos        `protobuf:"bytes,1,opt,name=postion" json:"postion,omitempty"`
	DbName           *string     `protobuf:"bytes,2,opt,name=dbName" json:"dbName,omitempty"`
	TableName        *string     `protobuf:"bytes,3,opt,name=tableName" json:"tableName,omitempty"`
	Type             *BinlogType `protobuf:"varint,4,opt,name=type,enum=protocol.BinlogType" json:"type,omitempty"`
	ColumnCount      *int32      `protobuf:"varint,5,opt,name=columnCount,def=0" json:"columnCount,omitempty"`
	PrimaryKey       []*Row      `protobuf:"bytes,6,rep,name=primaryKey" json:"primaryKey,omitempty"`
	Rows             []*Row      `protobuf:"bytes,7,rep,name=rows" json:"rows,omitempty"`
	XXX_unrecognized []byte      `json:"-"`
}

func (m *Binlog) Reset()                    { *m = Binlog{} }
func (m *Binlog) String() string            { return proto.CompactTextString(m) }
func (*Binlog) ProtoMessage()               {}
func (*Binlog) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

const Default_Binlog_ColumnCount int32 = 0

func (m *Binlog) GetPostion() *Pos {
	if m != nil {
		return m.Postion
	}
	return nil
}

func (m *Binlog) GetDbName() string {
	if m != nil && m.DbName != nil {
		return *m.DbName
	}
	return ""
}

func (m *Binlog) GetTableName() string {
	if m != nil && m.TableName != nil {
		return *m.TableName
	}
	return ""
}

func (m *Binlog) GetType() BinlogType {
	if m != nil && m.Type != nil {
		return *m.Type
	}
	return BinlogType_INSERT
}

func (m *Binlog) GetColumnCount() int32 {
	if m != nil && m.ColumnCount != nil {
		return *m.ColumnCount
	}
	return Default_Binlog_ColumnCount
}

func (m *Binlog) GetPrimaryKey() []*Row {
	if m != nil {
		return m.PrimaryKey
	}
	return nil
}

func (m *Binlog) GetRows() []*Row {
	if m != nil {
		return m.Rows
	}
	return nil
}

type Row struct {
	ColumnName       *string `protobuf:"bytes,1,opt,name=column_name,json=columnName" json:"column_name,omitempty"`
	ColumnType       *string `protobuf:"bytes,2,opt,name=column_type,json=columnType" json:"column_type,omitempty"`
	ColumnValue      *string `protobuf:"bytes,3,opt,name=column_value,json=columnValue" json:"column_value,omitempty"`
	Sql              *string `protobuf:"bytes,4,opt,name=sql" json:"sql,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *Row) Reset()                    { *m = Row{} }
func (m *Row) String() string            { return proto.CompactTextString(m) }
func (*Row) ProtoMessage()               {}
func (*Row) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func (m *Row) GetColumnName() string {
	if m != nil && m.ColumnName != nil {
		return *m.ColumnName
	}
	return ""
}

func (m *Row) GetColumnType() string {
	if m != nil && m.ColumnType != nil {
		return *m.ColumnType
	}
	return ""
}

func (m *Row) GetColumnValue() string {
	if m != nil && m.ColumnValue != nil {
		return *m.ColumnValue
	}
	return ""
}

func (m *Row) GetSql() string {
	if m != nil && m.Sql != nil {
		return *m.Sql
	}
	return ""
}

type Pos struct {
	BinlogFile       *string `protobuf:"bytes,1,opt,name=binlogFile" json:"binlogFile,omitempty"`
	Pos              *uint64 `protobuf:"varint,2,opt,name=pos" json:"pos,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *Pos) Reset()                    { *m = Pos{} }
func (m *Pos) String() string            { return proto.CompactTextString(m) }
func (*Pos) ProtoMessage()               {}
func (*Pos) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

func (m *Pos) GetBinlogFile() string {
	if m != nil && m.BinlogFile != nil {
		return *m.BinlogFile
	}
	return ""
}

func (m *Pos) GetPos() uint64 {
	if m != nil && m.Pos != nil {
		return *m.Pos
	}
	return 0
}

func init() {
	proto.RegisterType((*Binlog)(nil), "protocol.Binlog")
	proto.RegisterType((*Row)(nil), "protocol.Row")
	proto.RegisterType((*Pos)(nil), "protocol.Pos")
	proto.RegisterEnum("protocol.BinlogType", BinlogType_name, BinlogType_value)
}

func init() { proto.RegisterFile("binlog.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 345 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0x6c, 0x52, 0x51, 0x4b, 0x83, 0x50,
	0x18, 0xed, 0xaa, 0x73, 0xed, 0xdb, 0x0a, 0xf9, 0x88, 0xf2, 0x21, 0xca, 0xad, 0x87, 0x24, 0x68,
	0xc4, 0x5e, 0xa2, 0xde, 0x5a, 0x1a, 0x44, 0x63, 0xc8, 0xcd, 0x7a, 0x0d, 0xb7, 0x24, 0x04, 0xe7,
	0x67, 0xea, 0x1a, 0x42, 0x3f, 0xa0, 0x9f, 0x1d, 0xf7, 0x3a, 0xe7, 0x82, 0x9e, 0x3c, 0xf7, 0x9c,
	0x73, 0xbf, 0xe3, 0xf9, 0x14, 0x7a, 0xb3, 0x28, 0x89, 0xe9, 0x63, 0x98, 0x66, 0x54, 0x10, 0xee,
	0xca, 0xc7, 0x9c, 0xe2, 0xc1, 0x8f, 0x02, 0xfa, 0x58, 0x4a, 0x78, 0x0e, 0xed, 0x94, 0xf2, 0x22,
	0xa2, 0xc4, 0x64, 0x16, 0xb3, 0xbb, 0xa3, 0xbd, 0x61, 0x6d, 0x1b, 0x7a, 0x94, 0xf3, 0x5a, 0xc5,
	0x43, 0xd0, 0xdf, 0x67, 0xd3, 0x60, 0x11, 0x9a, 0x8a, 0xc5, 0xec, 0x0e, 0x5f, 0x9f, 0xf0, 0x18,
	0x3a, 0x45, 0x30, 0x8b, 0x43, 0x29, 0xa9, 0x52, 0x6a, 0x08, 0xb4, 0x41, 0x2b, 0xca, 0x34, 0x34,
	0x35, 0x8b, 0xd9, 0xfb, 0xa3, 0x83, 0x66, 0x76, 0x15, 0xef, 0x97, 0x69, 0xc8, 0xa5, 0x03, 0xcf,
	0xa0, 0x3b, 0xa7, 0x78, 0xb9, 0x48, 0xee, 0x69, 0x99, 0x14, 0x66, 0xcb, 0x62, 0x76, 0xeb, 0x96,
	0x5d, 0xf1, 0x6d, 0x16, 0x2f, 0x01, 0xd2, 0x2c, 0x5a, 0x04, 0x59, 0xf9, 0x14, 0x96, 0xa6, 0x6e,
	0xa9, 0x7f, 0x5f, 0x98, 0xd3, 0x8a, 0x6f, 0x19, 0xb0, 0x0f, 0x5a, 0x46, 0xab, 0xdc, 0x6c, 0xff,
	0x67, 0x94, 0xd2, 0xe0, 0x1b, 0x54, 0x4e, 0x2b, 0x3c, 0xad, 0xd3, 0xdf, 0x12, 0xd1, 0x83, 0xc9,
	0x1e, 0x50, 0x51, 0xb2, 0x48, 0x63, 0x90, 0x7d, 0x94, 0x6d, 0x83, 0x68, 0x81, 0x7d, 0xe8, 0xad,
	0x0d, 0x5f, 0x41, 0xbc, 0xac, 0x57, 0xb1, 0xbe, 0xf4, 0x2a, 0x28, 0x34, 0x40, 0xcd, 0x3f, 0x63,
	0xb9, 0x8b, 0x0e, 0x17, 0x70, 0x70, 0x0d, 0xaa, 0x47, 0x39, 0x9e, 0x00, 0x54, 0x5f, 0xea, 0x21,
	0x8a, 0x37, 0xe1, 0x0d, 0x23, 0x2e, 0xa6, 0x94, 0xcb, 0x50, 0x8d, 0x0b, 0x78, 0x71, 0x03, 0xd0,
	0x6c, 0x10, 0x01, 0xf4, 0xc7, 0xe9, 0xb3, 0xcb, 0x7d, 0x63, 0x47, 0xe0, 0x17, 0xcf, 0xb9, 0xf3,
	0x5d, 0x83, 0x09, 0xec, 0xb8, 0x13, 0xd7, 0x77, 0x0d, 0x05, 0xdb, 0xa0, 0x3a, 0xce, 0xc4, 0x50,
	0xc7, 0x47, 0xb0, 0xf9, 0x11, 0xc6, 0xdd, 0x6a, 0x88, 0x27, 0xce, 0xbf, 0x01, 0x00, 0x00, 0xff,
	0xff, 0x3c, 0xe6, 0xdc, 0x73, 0x2e, 0x02, 0x00, 0x00,
}